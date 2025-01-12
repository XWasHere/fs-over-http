package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/valyala/fasthttp"
	"io/fs"
	"io/ioutil"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

var (
	addr         = flag.String("addr", "localhost:6060", "TCP address to listen to")
	compress     = flag.Bool("compress", true, "Whether to enable transparent response compression")
	maxBodySize  = flag.Int("maxbodysize", 100*1024*1024, "MaxRequestBodySize, defaults to 100MiB")
	authToken    = []byte(ReadFileUnsafe("token", true))
	fsFolder     = "filesystem/"
	publicFolder = "filesystem/public/"
	privateDirs  = ReadNonEmptyLines("private_folders", publicFolder)
	ownerPerm    = os.FileMode(0700)
)

func main() {
	flag.Parse()
	//goland:noinspection ALL
	log.Printf("- Running fs-over-http on http://" + *addr)

	// If fsFolder or publicFolder don't exist
	SafeMkdir(fsFolder)
	SafeMkdir(publicFolder)

	// TODO: Switch over to using something similar to https://github.com/alessiosavi/GoDiffBinary/blob/7a8d35a20e38b14268b9840a4f9631f537a4dfea/api/api.go#L15
	// Instead of the manual RequestHandler that we have going

	// The gzipHandler will serve a compress request only if the client request it with headers (Content-Type: gzip, deflate)
	// Compress data before sending (if requested by the client)
	h := fasthttp.CompressHandlerLevel(RequestHandler, fasthttp.CompressBestCompression)
	s := &fasthttp.Server{
		Handler:            h,
		Name:               "fs-over-http",
		MaxRequestBodySize: *maxBodySize,
	}

	// Custom listenAndServe function in order to change `network` from `tcp4` to `tcp` (in order to allow tcp6)
	listenAndServe := func(addr string) error {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		if tcpListener, ok := ln.(*net.TCPListener); ok {
			return s.Serve(tcpKeepaliveListener{
				TCPListener:     tcpListener,
				keepalive:       s.TCPKeepalive,
				keepalivePeriod: s.TCPKeepalivePeriod,
			})
		}
		return s.Serve(ln)
	}

	if err := listenAndServe(*addr); err != nil {
		log.Fatalf("- Error in ListenAndServe: %s", err)
	}
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe, ListenAndServeTLS and
// ListenAndServeTLSEmbed so dead TCP connections (e.g. closing laptop mid-download)
// eventually go away.
type tcpKeepaliveListener struct {
	*net.TCPListener
	keepalive       bool
	keepalivePeriod time.Duration
}

func (ln tcpKeepaliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	if err := tc.SetKeepAlive(ln.keepalive); err != nil {
		tc.Close() //nolint:errcheck
		return nil, err
	}
	if ln.keepalivePeriod > 0 {
		if err := tc.SetKeepAlivePeriod(ln.keepalivePeriod); err != nil {
			tc.Close() //nolint:errcheck
			return nil, err
		}
	}
	return tc, nil
}

func RequestHandler(ctx *fasthttp.RequestCtx) {
	// The authentication key provided with said Auth header
	auth := ctx.Request.Header.Peek("Auth")

	// requestPath is prefixed with a /
	path := TrimFirstRune(string(ctx.Path()))
	filePath := fsFolder + path

	if len(auth) == 0 && ctx.IsGet() {
		filePath = publicFolder + path

		if Contains(privateDirs, RemoveLastRune(filePath, '/')) {
			HandleGeneric(ctx, fasthttp.StatusNotFound, "Not Found")
			return
		}

		HandleServeFile(ctx, filePath, true)
		return
	}

	// Make sure Auth key is correct
	if !bytes.Equal(auth, authToken) {
		HandleForbidden(ctx)
		return
	}

	switch string(ctx.Request.Header.Method()) {
	case fasthttp.MethodGet:
		HandleServeFile(ctx, filePath, false)
	case fasthttp.MethodPost:
		HandlePostRequest(ctx, filePath)
	case fasthttp.MethodPut:
		HandleAppendFile(ctx, filePath)
	case fasthttp.MethodDelete:
		HandleDeleteFile(ctx, filePath)
	default:
		HandleForbidden(ctx)
	}
}

func HandlePostRequest(ctx *fasthttp.RequestCtx, path string) {
	// If the dir key was provided, create that directory inside fsFolder
	dir := ctx.FormValue("dir")
	if len(dir) > 0 {
		dirStr := string(dir)
		// Remove all "/" and "." before the path
		for {
			dirStr = RemoveFirstRune(dirStr, '/')
			dirStr = RemoveFirstRune(dirStr, '.')
			if !strings.HasPrefix(dirStr, "/") && !strings.HasPrefix(dirStr, ".") {
				break
			}
		}

		// Add fsFolder as a prefix, we don't want the user to be able to make folders outside of it
		dirStr = fsFolder + dirStr
		err := os.MkdirAll(dirStr, ownerPerm)

		if err != nil {
			HandleInternalServerError(ctx, err)
			return
		}

		PrintResponsePath(ctx, path, true)
		return
	}

	// If not making a directory, don't allow writing directly to fsFolder
	if path == fsFolder {
		HandleModifyFsFolder(ctx)
		return
	}

	// If a file was provided, save it and return
	fh, err := ctx.FormFile("file")
	if err == nil {
		err = fasthttp.SaveMultipartFile(fh, path)

		if err != nil {
			HandleInternalServerError(ctx, err)
			return
		}

		PrintResponsePath(ctx, path, false)
		return
	}

	// If the content key was provided, write to said file
	content := ctx.FormValue("content")
	if len(content) > 0 {
		err = WriteToFile(path, strings.ReplaceAll(string(content), "\\n", "\n"))

		if err != nil {
			HandleInternalServerError(ctx, err)
			return
		}

		PrintResponsePath(ctx, path, false)
		return
	}

	// If none of the if statements passed, send a 400
	HandleGeneric(ctx, fasthttp.StatusBadRequest, "Missing 'file' or 'dir' or 'content' form")
}

func HandleServeFile(ctx *fasthttp.RequestCtx, path string, public bool) {
	isDir, err := IsDirectory(path)

	if err != nil {
		HandleInternalServerError(ctx, err)
		return
	}

	if isDir {
		path = AddLastRune(path, '/')

		files, err := ioutil.ReadDir(path)

		if err != nil {
			HandleInternalServerError(ctx, err)
			return
		}

		// Don't list private folders
		if public {
			filter := func(s fs.FileInfo) bool { return !Contains(privateDirs, RemoveLastRune(path+s.Name(), '/')) }
			files = Filter(files, filter)
		}
		
		filesAmt := len(files)
		// No files in dir
		if filesAmt == 0 {
			fmt.Fprintf(ctx, "%s\n\n", path)
			fmt.Fprintf(ctx, "0 directories, 0 files\n")
			return
		}

		sortType := ctx.QueryArgs().Peek("sort")

		if string(sortType) == "date" {
			// TODO: Make file sorting customizable
			// Sort files by date
			sort.Slice(files, func(i, j int) bool {
				return files[i].ModTime().Before(files[j].ModTime())
			})
		}

		// Sort to put folders first
		sort.SliceStable(files, func(i, j int) bool { return files[i].IsDir() && !files[j].IsDir() })

		// Print path name
		fmt.Fprintf(ctx, "%s\n", path)

		tFiles, tFolders, cFile := 0, 0, 0
		lineRune := "├── "

		// Print each file
		for _, f := range files {
			cFile++
			fn := f.Name()

			if f.IsDir() {
				tFolders++
				fn = AddLastRune(fn, '/')
			} else {
				tFiles++
			}

			// Fix line rune for last file
			if cFile == filesAmt {
				lineRune = "└── "
			}

			fmt.Fprintf(ctx, "%s%s\n", lineRune, fn)
		}

		// Print total dirs and files
		fmt.Fprintf(
			ctx, "\n%s, %s\n",
			Grammar(tFolders, "directory", "directories"),
			Grammar(tFiles, "file", "files"))

		return
	}

	content, err := ReadFile(path)

	// File is empty
	if len(content) == 0 {
		ctx.Response.SetStatusCode(fasthttp.StatusNoContent)
		return
	}

	if err != nil {
		HandleInternalServerError(ctx, err)
		return
	}

	// Open the file and handle errors
	f, err := os.Open(path)
	if err != nil {
		HandleInternalServerError(ctx, err)
		return
	}
	defer f.Close()

	// Get the contentType
	contentType, err := GetFileContentTypeExt(f, path)
	if err != nil {
		HandleInternalServerError(ctx, err)
		return
	}

	// Serve the file itself
	ctx.Response.Header.Set(fasthttp.HeaderContentType, contentType)
	fmt.Fprint(ctx, content)
}

func HandleAppendFile(ctx *fasthttp.RequestCtx, path string) {
	if path == fsFolder {
		HandleModifyFsFolder(ctx)
		return
	}

	content := ctx.FormValue("content")
	// If the content key was not provided, return an error
	if len(content) == 0 {
		HandleGeneric(ctx, fasthttp.StatusBadRequest, "Missing 'content' form")
		return
	}

	contentStr := string(content) + "\n"
	oldContent, err := ReadFile(path)

	if err == nil {
		contentStr = oldContent + contentStr
	}

	err = WriteToFile(path, contentStr)

	if err != nil {
		HandleInternalServerError(ctx, err)
		return
	}

	PrintResponsePath(ctx, path, false)
}

func HandleDeleteFile(ctx *fasthttp.RequestCtx, path string) {
	if path == fsFolder {
		HandleModifyFsFolder(ctx)
		return
	}

	if _, err := os.Stat(path); err == nil {
		err = os.Remove(path)

		if err != nil {
			HandleInternalServerError(ctx, err)
		} else {
			PrintResponsePath(ctx, path, false)
		}
	} else {
		HandleInternalServerError(ctx, err)
	}
}

func PrintResponsePath(ctx *fasthttp.RequestCtx, path string, folder bool) {
	ctx.Response.Header.Set("X-Server-Message", "200 Success")
	path = strings.TrimPrefix(path, "filesystem/")

	if folder {
		ctx.Response.Header.Set("X-Modified-Path", AddLastRune(path, '/')+"\n")
	} else {
		ctx.Response.Header.Set("X-Modified-Path", RemoveLastRune(path, '/')+"\n")
	}
}

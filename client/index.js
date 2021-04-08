const https = require("https");

const RUNE_MIME = "text/filelist";
const SERVER = "i.l1v.in";

const ERRORS = {
    ERR_FORBIDDEN: 0
}

const MESSAGES = {
    ACCESSDENIED: "Access Denied"
};

class AccessDeniedError extends Error{
    constructor() {
        super()
    }
}
/**
 * A simple Directory structure
 * @typedef Directory
 * @property {string} path Path to the directory
 * @property {string[]} files Files in the directory
 * @property {string[]} directories Directories in the directory
 * @property {Number} fileCount File count
 * @property {Number} dirCount Directory Count
 */

/**
 * Parses a directory response from the server into something more computer friendly
 * @param {string} data
 * @returns {Promise<Directory>}
 */
function parseRuneStruct(data) {
    return new Promise((res,rej) => {
        if (/403 Forbidden/.test(data)) {rej(new AccessDeniedError()); return;}

        let lines = data.split("\n");

        let path = `/${lines.shift()}`;
        
        lines.pop();
        let count= lines.pop();
        lines.pop();

        let counts= count.split(",");
        let dirCount = parseInt(counts[0].replace(/ (directory|directories)/,""), 10);
        let fileCount= parseInt(counts[1].replace(/ (file|files)/,""), 10);
        
        let files = [];
        let dirs  = [];
        lines.forEach((v) => {
            let name = v.replace(/(└|├)── /,"");
            
            if (name.endsWith("/"))
                dirs.push(name.replace("/",""));
            else
                files.push(name);
        });

        res({
            path: path,
            fileCount: fileCount,
            dirCount: dirCount,
            files: files,
            directories: dirs
        });
    });
}

/**
 * @param {String} path 
 * @returns {Promise<Directory>}
 */
function getDir(opts) {
    return new Promise(async (res, rej) => {
        let req = https.request({
            method: "GET",
            host: opts.host,
            path: opts.path
        }, (r) => r.on("data", (d) => {
            parseRuneStruct(d.toString()).then(res,rej);
        }));

        req.end();
    });
}

async function main() {
    let cwdstk = ["/"];
    function cwd() {
        return cwdstk.join("/");
    }

    let server = "";
    let connected = false;

    process.stdout.write('> ');
    process.stdin.on("data", async (data) => {
        let input = data.toString().trim().split(' ');
        
        switch (input[0].toLowerCase()) {
            case "connect":
                server = input[1];
                connected = 1;
                // TODO: Add validation code.
                process.stdout.write("Connected\n");
                break;
            case "pwd":
                if (!connected) {
                    process.stdout.write("Error: Not connected\n");
                    break;
                }
                process.stdout.write(`${cwd()}\n`);
                break;
            case "ls":
                if (!connected) {
                    process.stdout.write("Error: Not connected\n");
                    break;
                }

                let fail = false;
                let dir;
                try {
                    dir = await getDir({
                        host: server,
                        path: cwd()
                    });
                }
                catch (err) {
                    if (err instanceof AccessDeniedError) {
                        process.stdout.write("Error: Access Denied\n");
                    }
                    fail = true;
                }

                if (fail) break;
                dir.directories.forEach(d => {
                    process.stdout.write(`${d}/\n`);
                });
                dir.files.forEach(f => {
                    process.stdout.write(`${f}\n`);
                });
                break;
            case "cd":
                if (input[1] == "..") cwdstk.pop();
                else                  cwdstk.push(input[1]);
                break;
            default:
                process.stdout.write("Error: Invalid Command\n");
                break;
        }

        process.stdout.write('> ');
    })
}

main();
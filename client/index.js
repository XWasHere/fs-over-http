const https = require("https");

const RUNE_MIME = "text/filelist";
const SERVER = "i.l1v.in";

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
 * @returns {Directory}
 */
function parseRuneStruct(data) {
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

    return {
        path: path,
        fileCount: fileCount,
        dirCount: dirCount,
        files: files,
        directories: dirs
    };
}

/**
 * @param {String} path 
 * @returns {Directory}
 * @ignore
 */
function getDir(path) {
    return new Promise((res, rej) => {
        let req = https.request({
            method: "GET",
            host: SERVER,
            path: path
        }, (r) => r.on("data", (d) => {
            res(parseRuneStruct(d.toString()));
        }));

        req.end();
    });
}

getDir('/').then(console.log);

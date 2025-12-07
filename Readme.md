# basefs - Base File System

[![Go Reference](https://pkg.go.dev/badge/github.com/absfs/basefs.svg)](https://pkg.go.dev/github.com/absfs/basefs)
[![Go Report Card](https://goreportcard.com/badge/github.com/absfs/basefs)](https://goreportcard.com/report/github.com/absfs/basefs)
[![CI](https://github.com/absfs/basefs/actions/workflows/ci.yml/badge.svg)](https://github.com/absfs/basefs/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

The `basefs` package implements the `absfs.FileSystem` interface by constraining a `absfs.FileSystem` to a specific subdirectory. A basefs filesystem will essentially be re-rooted to the provided subdirectory and no path navigation will allow access to paths outside of the subdirectory. All paths the are passed to the underlying filesystem are absolute paths constructed by the basefs to avoid ambiguity. After constructing the correct path basefs calls the underlying Filesystem methods normally. `basefs` will also edit any errors returned to reflect the path as it would appear if the prefix was removed.

The `basefs` package also provides `Prefix` and `Unwrap` functions for debugging purposes.


## Install 

```bash
$ go get github.com/absfs/basefs
```

## Example Usage

```go
package main

import (
    "fmt"

    "github.com/absfs/basefs"
    "github.com/absfs/osfs"
)

func main() {
    ofs, _ := osfs.NewFS() // remember kids don't ignore errors
    bfs, _ := basefs.NewFS(ofs, "/tmp")
    f, _ := bfs.Create("/test.txt")
    defer bfs.Remove("/test.txt")
    bfsdata := []byte("base fs bound to `/tmp`\n")
    f.Write(bfsdata)
    f.Close()

    f, _ = ofs.Open("/tmp/test.txt")
    defer f.Close()
    ofsdata := make([]byte, 512)
    n, _ := f.Read(ofsdata)
    ofsdata = ofsdata[:n]

    if string(bfsdata) == string(ofsdata) {
        fmt.Println("it's the same file.")
    }
}
```

## absfs
Check out the [`absfs`](https://github.com/absfs/absfs) repo for more information about the abstract filesystem interface and features like filesystem composition 

## LICENSE

This project is governed by the MIT License. See [LICENSE](https://github.com/absfs/basefs/blob/master/LICENSE)




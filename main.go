// Copyright 2015 Vilibald Wanča. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package main

import (
    "bazil.org/fuse"
    "bazil.org/fuse/fs"
    "flag"
    "fmt"
    log "github.com/Sirupsen/logrus"
    "os"
    "os/user"
    "path"
    "strconv"
)

const (
    defaultDir = ".grivefs"
)

// enable fuse logging of debug messages to stderr.
var fusedebug = flag.Bool("fusedebug", false, "enable fuse debugging to stderr")
var verbose = flag.Bool("v", false, "enable debugging messages to stderr")
var dir = flag.String("dir", "",
    "set the grivefs cache and config directory, default is ~/.grivefs")

var Usage = func() {
    fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
    fmt.Fprintf(os.Stderr, "  %s MOUNTPOINT\n", os.Args[0])
    flag.PrintDefaults()
}

func init() {
    // Output to stderr instead of stdout
    log.SetOutput(os.Stderr)

    // Only log the warning severity or above.
    log.SetLevel(log.InfoLevel)
}

func debugLog(msg interface{}) {
    log.Debug("%v\n", msg)
}

func main() {
    var err error
    var conf *Config

    flag.Usage = Usage
    flag.Parse()

    if flag.NArg() != 1 {
        Usage()
        os.Exit(2)
    }

    usr, err := user.Current()
    if err != nil {
        log.Fatal(err)
    }

    if *verbose {
        log.SetLevel(log.DebugLevel)
    }

    if *dir != "" {
        conf, err = Initialize(*dir)
    } else {
        conf, err = Initialize(path.Join(usr.HomeDir, defaultDir))
    }

    if err != nil {
        log.Fatal(err)
    }

    uid, _ := strconv.Atoi(usr.Uid)
    gid, _ := strconv.Atoi(usr.Gid)
    f, err := MakeGriveFS(conf, uint32(uid), uint32(gid))
    if err != nil {
        log.Fatal(err)
    }
    defer f.Destroy()

    mountpoint := flag.Arg(0)
    c, err := fuse.Mount(
        mountpoint,
        fuse.FSName("grivefs"),
        fuse.Subtype("googledrivefs"),
        fuse.LocalVolume(),
        fuse.VolumeName("Google Drive FS"),
        fuse.ReadOnly(),
    )
    defer c.Close()
    if err != nil {
        log.Fatal(err)
    }
    log.Infof("Mounting to %s", mountpoint)
    server := fs.Server{
        FS: f,
    }

    if *fusedebug {
        server.Debug = debugLog
    }
    log.Info("Starting to serve FUSE requests")
    err = server.Serve(c)
    if err != nil {
        log.Fatal(err)
    }

    <-c.Ready
    if err := c.MountError; err != nil {
        log.Fatal(err)
    }
    log.Info("FUSE server stopped")
}

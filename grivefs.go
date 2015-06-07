// Copyright 2015 Vilibald Wanča. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

// +build linux

package main

import (
    "bazil.org/fuse"
    "bazil.org/fuse/fs"
    "errors"
    log "github.com/Sirupsen/logrus"
    "golang.org/x/net/context"
    drive "google.golang.org/api/drive/v2"
    "io/ioutil"
    "os"
    "path"
    "sync"
    "sync/atomic"
    "time"
)

const (
    BSize     = 512
    SmallFile = 65536
)

type grvNode struct {
    sync.RWMutex
    attr fuse.Attr

    name   string
    fs     *griveFS
    rf     *drive.File
    parent *grvDir
}

type grvDir struct {
    grvNode
    nodes map[string]fs.Node
}

type grvFile struct {
    grvNode
    fetcher *fileFetcher
}

type griveFS struct {
    c         *Config
    Uid       uint32
    Gid       uint32
    remote    *Remote
    nodeCount uint64
    nodeId    uint64
    size      uint64
    files     uint32
    dirs      uint32
    root      *grvDir
    done      chan int
}

func MakeGriveFS(c *Config, uid uint32, gid uint32) (*griveFS, error) {
    logger := log.WithField("func", "grivefs.go:MakeGriveFS")
    logger.Info("Connecting ....")
    r, err := MakeRemote(c)
    if err != nil {
        return nil, err
    }
    g := &griveFS{c, uid, gid, r, 0, 0, 0, 0, 0, nil, make(chan int, 0)}

    f, err := r.GetRootFile()
    if err != nil {
        return nil, err
    }
    logger.Info("Loading structure ...")
    g.root = g.newDir(f, nil)
    if g.root == nil {
        return nil, errors.New("Could Not create root directory")
    }
    logger.Info("... loaded")
    ticker := time.NewTicker(time.Duration(g.c.CacheCleanT) * time.Second)
    go func() {
        logger.Info("Cache cleaner started")
        for {
            select {
            case <-ticker.C:
                go g.cleanCache()
            case <-g.done:
                ticker.Stop()
                logger.Info("Cache cleaner stopped")
                return
            }
        }
    }()
    return g, nil
}

//
func (g *griveFS) cleanCache() {
    logger := log.WithFields(log.Fields{
        "func": "grivefs.go:cleanCache",
        "dir":  g.c.DataDir})
    logger.Debug("Cleaning cache...")
    fs, err := ioutil.ReadDir(g.c.DataDir)
    if err != nil {
        logger.Error(err)
        return
    }
    for _, fi := range fs {
        if fi.Name()[0] == '.' {
            continue
        }
        atm := atime(fi)
        if !fi.IsDir() && int(time.Since(atm).Hours()) > g.c.CacheTTL {
            logger.WithField("file", fi.Name()).Debug("Removing old file")
            err := os.Remove(path.Join(g.c.DataDir, fi.Name()))
            if err != nil {
                logger.WithField("file", fi.Name()).Warn(err)
            }
        }
    }
    logger.Debug("Cleaning cache done.")
}

//
func (g *griveFS) Destroy() {
    log.Info("Unmount ... shuting down")
    g.done <- 1
    log.Info("Unmount ... done")
}

func (g *griveFS) nextId() uint64 {
    return atomic.AddUint64(&g.nodeId, 1)
}

func (g *griveFS) newDir(f *drive.File, p *grvDir) *grvDir {
    logger := log.WithFields(log.Fields{
        "func": "grivefs.go:newDir",
        "dir":  f.Title})
    if p != nil {
        logger.Debugf("Creating new directory, parent %s", p.name)
    } else {
        logger.Debug("Creating root directory")
    }

    ctime, mtime, atime := fileTimes(f)

    dir := &grvDir{
        grvNode: grvNode{
            attr: fuse.Attr{
                Inode:  g.nextId(),
                Size:   BSize,
                Blocks: 1,
                Atime:  atime,
                Mtime:  mtime,
                Ctime:  ctime,
                Crtime: ctime,
                Mode:   fileMode(f),
                Uid:    g.Uid,
                Gid:    g.Gid,
            },
            name:   f.Title,
            fs:     g,
            rf:     f,
            parent: p,
        },
        nodes: make(map[string]fs.Node),
    }
    g.dirs++

    err := dir.loadDirContent()
    if err != nil {
        logger.Error(err)
    }

    return dir
}

func (g *griveFS) newFile(f *drive.File, p *grvDir) *grvFile {
    log.WithFields(log.Fields{
        "func": "grivefs.go:newFile",
        "dir":  p.name,
        "file": f.Title}).Debug("Creating new file")
    ctime, mtime, atime := fileTimes(f)

    g.files++
    gf := &grvFile{
        grvNode: grvNode{
            attr: fuse.Attr{
                Inode:  g.nextId(),
                Size:   uint64(f.FileSize),
                Blocks: uint64(f.FileSize) / BSize,
                Atime:  atime,
                Mtime:  mtime,
                Ctime:  ctime,
                Crtime: ctime,
                Mode:   fileMode(f),
                Uid:    g.Uid,
                Gid:    g.Gid,
            },
            name:   f.Title,
            fs:     g,
            rf:     f,
            parent: p,
        },
        fetcher: MakeFileFetcher(g.c.DataDir, f),
    }

    if RemoteIsDesktopFile(gf.rf) {
        gf.attr.Size = uint64(len(DesktopFileContent(gf.rf)))
        gf.attr.Blocks = gf.attr.Size / BSize
    }

    return gf
}

func (g *griveFS) Statfs(ctx context.Context, req *fuse.StatfsRequest,
    resp *fuse.StatfsResponse) error {
    log.WithField("func", "grivefs.go:Statfs").Debug("Statfs")
    resp.Blocks = uint64((atomic.LoadUint64(&g.size) + BSize - 1) / BSize)
    resp.Bsize = BSize
    resp.Files = atomic.LoadUint64(&g.nodeCount)
    return nil
}

func (g *griveFS) Root() (fs.Node, error) {
    return g.root, nil
}

func (n *grvNode) Attr(o *fuse.Attr) {
    n.RLock()
    *o = n.attr
    n.RUnlock()
}

func (d *grvDir) loadDirContent() error {

    if len(d.nodes) == 0 {
        fs, err := d.fs.remote.ListDir(d.rf)
        if err != nil {
            return err
        }

        for _, f := range fs {
            if !(f.Labels.Trashed || f.Labels.Hidden) {
                if RemoteIsDir(f) {
                    d.nodes[f.Title] = d.fs.newDir(f, d)
                    log.WithField("func", "grivefs.go:loadDirContent").
                        Debugf("adding subdirectory: %s", f.Title)
                } else {
                    d.nodes[f.Title] = d.fs.newFile(f, d)
                    log.WithField("func", "grivefs.go:loadDirContent").
                        Debugf("adding file %s", f.Title)
                }
            }
        }
    }

    return nil
}

func (d *grvDir) Lookup(ctx context.Context, name string) (fs.Node, error) {
    d.RLock()
    log.WithField("func", "grivefs.go:Lookup").Debugf("Lookup %s", name)

    n, exist := d.nodes[name]
    d.RUnlock()

    if !exist {
        return nil, fuse.ENOENT
    }
    return n, nil
}

func (d *grvDir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
    d.RLock()
    log.WithField("func", "grivefs.go:ReadDirAll").Debugf("ReadDirAll %s", d.name)

    // err := d.update()
    // if err != nil {
    //     log.Infof("GRIVEFS: Update dir %s failed %v", d.name, err)
    //     return nil, fuse.ENOENT
    // }

    dirs := make([]fuse.Dirent, len(d.nodes)+2)
    // Add special references.
    dirs[0] = fuse.Dirent{
        Name:  ".",
        Inode: d.attr.Inode,
        Type:  fuse.DT_Dir,
    }
    dirs[1] = fuse.Dirent{
        Name: "..",
        Type: fuse.DT_Dir,
    }

    if d.parent != nil {
        dirs[1].Inode = d.parent.attr.Inode
    } else {
        dirs[1].Inode = d.attr.Inode
    }

    // Add remaining files.
    idx := 2
    for name, node := range d.nodes {
        ent := fuse.Dirent{
            Name: name,
        }
        switch n := node.(type) {
        case *grvFile:
            ent.Inode = n.attr.Inode
            ent.Type = fuse.DT_File
        case *grvDir:
            ent.Inode = n.attr.Inode
            ent.Type = fuse.DT_Dir
        }
        dirs[idx] = ent
        idx++
    }
    d.RUnlock()
    return dirs, nil
}

//
// func (d *grvDir) update() error {

//     if time.Since(d.lastRefresh).Seconds() > minRefreshDelay {
//         log.Debug("GRIVEFS: dir update")
//         rf, err := d.fs.remote.GetFileInfo(d.rf.Id)
//         if err != nil {
//             return err
//         }
//         ctime, mtime, atime := fileTimes(rf)

//         if mtime != d.attr.Mtime {
//             log.Debug("GRIVEFS: dir outdated, refresh")
//             d.rf = rf
//             d.attr.Mtime = mtime
//             d.attr.Ctime = ctime
//             d.attr.Crtime = ctime
//             d.attr.Atime = atime
//             d.attr.Size = BSize
//             d.attr.Blocks = 1
//             d.name = rf.Title
//         }
//         d.lastRefresh = time.Now()
//     }
//     return nil
// }

//
func (d *grvDir) Create(ctx context.Context, req *fuse.CreateRequest,
    resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
    return nil, nil, fuse.EPERM
}

//
func (d *grvDir) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
    return fuse.EPERM
}

//
func (d *grvDir) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
    return nil, fuse.EPERM
}

//
func (d *grvDir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
    return fuse.EPERM
}

//
func (d *grvDir) rmfile(f *grvFile) {
    d.Lock()
    defer d.Unlock()
    delete(d.nodes, f.name)
}

//
func (d *grvDir) addfile(f *grvFile) {
    d.Lock()
    defer d.Unlock()
    d.nodes[f.name] = f
}

//
func (f *grvFile) Open(ctx context.Context, req *fuse.OpenRequest,
    resp *fuse.OpenResponse) (fs.Handle, error) {
    var err error
    f.Lock()
    defer f.Unlock()
    log.WithField("func", "grivefs.go:Open").Debugf("Open %s", f.name)
    // err = f.update()
    // if err != nil {
    //     return f, err
    // }
    err = f.fetcher.Open(f.fs.remote)
    return f, err
}

//
func (f *grvFile) Read(ctx context.Context, req *fuse.ReadRequest,
    resp *fuse.ReadResponse) error {
    f.RLock()
    defer f.RUnlock()
    log.WithFields(log.Fields{
        "func":       "grivefs.go:Read",
        "file":       f.name,
        "remot_file": f.rf.Id,
        "off":        req.Offset,
        "size":       req.Size,
    }).Debug("Read")

    resp.Data = make([]byte, req.Size)
    _, err := f.fetcher.Read(req.Offset, resp.Data)
    return err
}

//
func (f *grvFile) Flush(ctx context.Context, req *fuse.FlushRequest) error {
    log.WithField("func", "grivefs.go:Flush").Debugf("Flush %s", f.name)
    return nil
}

//
func (f *grvFile) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
    f.RLock()
    log.WithField("func", "grivefs.go:Release").Debugf("Release (close) %s", f.name)
    defer f.RUnlock()
    f.fetcher.Close()
    return nil
}

//
func (f *grvFile) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
    return fuse.EPERM
}

//
func (f *grvFile) Setattr(ctx context.Context, req *fuse.SetattrRequest,
    resp *fuse.SetattrResponse) error {
    return fuse.EPERM
}

//
// func (f *grvFile) update() error {

//     if time.Since(f.lastRefresh).Seconds() > minRefreshDelay {
//         log.Debug("GRIVEFS: file update %s", f.name)
//         rf, err := f.fs.remote.GetFileInfo(f.rf.Id)
//         if err != nil {
//             return err
//         }
//         // Was the file deleted?
//         if rf.Labels.Trashed {
//             f.parent.rmfile(f)
//             deleteFileFetcher(f.fetcher)
//             f = nil
//             return fuse.ENOENT
//         }

//         ctime, mtime, atime := fileTimes(rf)

//         if mtime != f.attr.Mtime {
//             log.Debug("GRIVEFS: file outdated, refresh")
//             deleteFileFetcher(f.fetcher)
//             f.rf = rf
//             f.fetcher = MakeFileFetcher(f.fs.c.DataDir, rf)
//             f.attr.Mtime = mtime
//             f.attr.Ctime = ctime
//             f.attr.Crtime = ctime
//             f.attr.Atime = atime
//             f.attr.Size = uint64(rf.FileSize)
//             f.attr.Blocks = uint64(rf.FileSize) / BSize
//             f.parent.rmfile(f)
//             f.name = rf.Title
//             f.parent.addfile(f)
//         }
//         f.lastRefresh = time.Now()
//     }
//     return nil
// }

//
func fileTimes(f *drive.File) (time.Time, time.Time, time.Time) {

    mtime, err := time.Parse(time.RFC3339, f.ModifiedDate)
    if err != nil {
        log.Error(err)
    }
    ctime, err := time.Parse(time.RFC3339, f.CreatedDate)
    if err != nil {
        log.Error(err)
    }

    var atime time.Time
    if f.LastViewedByMeDate != "" {
        atime, err = time.Parse(time.RFC3339, f.LastViewedByMeDate)
        if err != nil {
            log.Warning(err)
            atime = mtime
        }
    } else {
        atime = mtime
    }

    return ctime, mtime, atime
}

//
func fileMode(f *drive.File) os.FileMode {
    m := os.FileMode(0440)

    if RemoteIsDir(f) {
        m = os.FileMode(0550) | os.ModeDir
    }

    return m
}

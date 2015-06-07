// Copyright 2015 Vilibald Wanča. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package main

import (
    "crypto/md5"
    "errors"
    "fmt"
    log "github.com/Sirupsen/logrus"
    "github.com/bartmeuris/progressio"
    drive "google.golang.org/api/drive/v2"
    "io"
    "os"
    "path"
    "strings"
    "time"
)

const (
    MinDownloadSize = 65536
)

type fileFetcher struct {
    localPath  string
    rf         *drive.File
    lf         *os.File
    opened     int
    inProgress int32
}

//
func MakeFileFetcher(dir string, rf *drive.File) *fileFetcher {
    f := &fileFetcher{
        localPath:  path.Join(dir, rf.Id),
        rf:         rf,
        lf:         nil,
        opened:     0,
        inProgress: 0,
    }
    return f
}

//
func deleteFileFetcher(f *fileFetcher) {
    if f.lf != nil {
        f.lf.Close()
    }
    os.Remove(f.localPath)
    f = nil
}

//
func (f *fileFetcher) Open(r *Remote) error {

    logger := log.WithFields(log.Fields{"func": "fetcher.go:Open", "file": f.localPath})
    var err error
    if _, err := os.Stat(f.localPath); os.IsNotExist(err) {
        if RemoteIsDesktopFile(f.rf) {
            err = f.makeDesktopFile()
            if err != nil {
                logger.Debugf("Failed to create Desktopfile %v", err)
                return err
            }
        } else {
            logger.Debug("File not stored locally, downloading")
            ready := make(chan error)
            go f.download(r, ready)
            logger.Debug("Waiting for download to be ready")
            err = <-ready
            if err != nil {
                logger.Warn(err)
                return err
            }
        }
    }

    if f.lf == nil {
        f.lf, err = os.Open(f.localPath)
        if err != nil {
            return err
        }
    }
    f.opened++

    return nil
}

func (f *fileFetcher) Close() {
    f.opened--
    if f.opened == 0 {
        log.WithFields(log.Fields{
            "func": "fetcher.go:Close",
            "file": f.localPath}).Debug("Closing file handle")
        f.lf.Close()
        f.lf = nil
    }
}

func (f *fileFetcher) download(r *Remote, ready chan error) {

    logger := log.WithFields(log.Fields{
        "func":        "fetcher.go:download",
        "file":        f.localPath,
        "remote_file": f.rf.Id})

    logger.Debug("Downloading remote file.")
    resp, err := r.Download(f.rf)
    if err != nil {
        logger.Warn(err)
        ready <- err
        return
    }
    out, err := os.OpenFile(f.localPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
    if err != nil {
        logger.Warn(err)
        ready <- err
        return
    }

    defer out.Close()
    hasher := md5.New()
    pw, ch := progressio.NewProgressWriter(out, f.rf.FileSize)
    defer pw.Close()

    go func() {
        sent := false
        for p := range ch {
            if p.Transferred > MinDownloadSize {
                ready <- nil
                sent = true
            }
        }
        if !sent {
            ready <- nil
        }
    }()

    w := io.MultiWriter(pw, hasher)
    n, err := io.Copy(w, resp)
    logger.Debugf("Downloaded %d bytes", n)
    if err != nil {
        logger.Warn(err)
        return
    }

    chksum := fmt.Sprintf("%s", string(hasher.Sum(nil)))
    if strings.EqualFold(chksum, f.rf.Md5Checksum) {
        logger.WithFields(log.Fields{
            "got":      chksum,
            "expected": f.rf.Md5Checksum}).Warn("Checksums don't match")
        //FIXME : delete the local file
    }
}

func (f *fileFetcher) locFileSize() int64 {

    fi, err := os.Stat(f.localPath)
    if err != nil {
        log.WithFields(log.Fields{
            "func": "fetcher.go:locFileSIze",
            "file": f.localPath}).Warn(err)
        return 0
    }
    return fi.Size()
}

//
func (f *fileFetcher) Read(off int64, b []byte) (int, error) {

    if f.lf == nil {
        return 0, errors.New(fmt.Sprintf("File %s not opened", f.localPath))
    }

    size := int64(len(b))
    cur := f.locFileSize()
    for cur < off+size && cur < f.rf.FileSize {
        time.Sleep(500 * time.Millisecond)
        cur = f.locFileSize()
    }

    _, err := f.lf.Seek(off, os.SEEK_SET)
    if err != nil {
        return 0, err
    }
    return f.lf.Read(b)
}

//
//
func (f *fileFetcher) makeDesktopFile() error {

    out, err := os.OpenFile(f.localPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
    defer out.Close()
    if err != nil {
        return err
    }
    out.WriteString(DesktopFileContent(f.rf))
    return nil
}

func DesktopFileContent(f *drive.File) string {
    return fmt.Sprintf("[Desktop Entry]\nIcon=%s\nName=%s\nType=Link\nURL=%s\n",
        strings.Replace(f.MimeType, "/", "-", -1), f.Title, f.AlternateLink)
}

//
func (f *fileFetcher) IsOpen() bool {
    return f.lf != nil
}

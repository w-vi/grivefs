// Copyright 2015 Vilibald Wanča. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package main

import (
    "errors"
    "fmt"
    log "github.com/Sirupsen/logrus"
    "golang.org/x/oauth2"
    drive "google.golang.org/api/drive/v2"
    "io"
    "net/http"
    "strings"
    "time"
)

const (
    mimeFolder           string = "application/vnd.google-apps.folder"
    mimeGoogleApps       string = "application/vnd.google-apps."
    Scope                       = "https://www.googleapis.com/auth/drive"
    RedirectURL                 = "urn:ietf:wg:oauth:2.0:oob"
    GoogleOAuth2AuthURL         = "https://accounts.google.com/o/oauth2/auth"
    GoogleOAuth2TokenURL        = "https://accounts.google.com/o/oauth2/token"
)

type Remote struct {
    *drive.Service
    c        *http.Client
    a        *drive.About
    requests uint64
}

func makeOAuthConfig(c *Config) *oauth2.Config {
    return &oauth2.Config{
        ClientID:     c.ClientId,
        ClientSecret: c.ClientSecret,
        Scopes:       []string{Scope},
        RedirectURL:  RedirectURL,
        Endpoint: oauth2.Endpoint{
            AuthURL:  GoogleOAuth2AuthURL,
            TokenURL: GoogleOAuth2TokenURL,
        },
    }
}

// Create new Remote object from the configuration provided, if there
// is no refresh token we need to user authorize the access to his
// drive first.
func MakeRemote(c *Config) (*Remote, error) {
    var tok *oauth2.Token
    var err error
    logger := log.WithField("func", "remote.go:MakeRemote")
    config := makeOAuthConfig(c)
    if c.RefreshToken == "" {
        logger.Info("Connecting to unauthorized drive ...")
        authUrl := config.AuthCodeURL("state", oauth2.AccessTypeOffline)
        fmt.Println("Please visit this URL to get an authorization code")
        fmt.Println(authUrl)
        fmt.Print("Paste the authorization code: ")
        var code string
        if _, err = fmt.Scan(&code); err != nil {
            logger.Fatal(err)
        }
        tok, err = config.Exchange(oauth2.NoContext, code)
        if err != nil {
            logger.Fatal(err)
        }
        c.RefreshToken = tok.RefreshToken
        c.Write()
    } else {
        logger.Info("Connecting to existing drive ...")
        tok = &oauth2.Token{RefreshToken: c.RefreshToken}
    }

    client := config.Client(oauth2.NoContext, tok)
    d, err := drive.New(client)
    if err != nil {
        return nil, err
    }
    a, err := d.About.Get().Do()
    return &Remote{d, client, a, 1}, err
}

func (d *Remote) GetRootFile() (*drive.File, error) {
    return d.GetFileInfo(d.a.RootFolderId)
}

// Gets all the drive.File items in the given dir
func (d *Remote) ListDir(dir *drive.File) ([]*drive.File, error) {
    var fs []*drive.File
    logger := log.WithFields(log.Fields{"func": "remote.go:ListDir", "dir": dir.Title})
    pageToken := ""
    logger.Debug("Listing directory")
    for {
        q := d.Files.List()
        q.Q(fmt.Sprintf("'%s' in parents", dir.Id))
        // If we have a pageToken set, apply it to the query
        if pageToken != "" {
            q = q.PageToken(pageToken)
        }
        r, err := q.Do()
        if err != nil {
            logger.Warn(err)
            return nil, err
        }
        fs = append(fs, r.Items...)
        pageToken = r.NextPageToken
        if pageToken == "" {
            break
        }
    }

    return fs, nil
}

// Get all the file meta data basicaly just call the drive to get the
// file object as described in
// https://developers.google.com/drive/v2/reference/files
func (d *Remote) GetFileInfo(fileId string) (*drive.File, error) {
    logger := log.WithFields(log.Fields{"func": "remote.go:GetFileInfo", "fileId": fileId})
    logger.Debug("GET file info")
    f, err := d.Files.Get(fileId).Do()
    if err != nil {
        logger.Warn(err)
        return nil, err
    }
    return f, nil
}

func (d *Remote) Download(f *drive.File) (io.ReadCloser, error) {
    logger := log.WithFields(log.Fields{"func": "remote.go:Download", "fileId": f.Id})
    if f.DownloadUrl == "" {
        // If there is no downloadUrl, there is no body
        err := errors.New("File is not downloadable")
        logger.Warn(err)
        return nil, err
    }

    logger.WithField("url", f.DownloadUrl).Debug("Downloading ...")
    resp, err := d.c.Get(f.DownloadUrl)
    if err != nil || resp.StatusCode < 200 || resp.StatusCode > 299 {
        return resp.Body, err
    }
    return resp.Body, nil
}

func RemoteIsDir(f *drive.File) bool {
    return f.MimeType == mimeFolder
}

func RemoteIsDesktopFile(f *drive.File) bool {
    return strings.HasPrefix(f.MimeType, mimeGoogleApps)
}

// utility function to print the drive.File struct
func PrintInfo(f *drive.File) {
    fields := map[string]string{
        "Id":          f.Id,
        "Title":       f.Title,
        "Description": f.Description,
        "Size":        FileSizeFormat(f.FileSize),
        "Created":     ISODateToLocal(f.CreatedDate),
        "Modified":    ISODateToLocal(f.ModifiedDate),
        "Accessed":    ISODateToLocal(f.LastViewedByMeDate),
        "Owner":       strings.Join(f.OwnerNames, ", "),
        "Md5sum":      f.Md5Checksum,
        "Mime-type":   f.MimeType,
        "Parents":     ParentList(f.Parents),
    }

    order := []string{
        "Id",
        "Title",
        "Description",
        "Size",
        "Created",
        "Modified",
        "Accessed",
        "Owner",
        "Md5sum",
        "Mime-type",
        "Parents",
    }
    InfoPrint(fields, order)
}

// Print list of drive.File parent references
func ParentList(parents []*drive.ParentReference) string {
    ids := make([]string, 0)
    for _, parent := range parents {
        ids = append(ids, parent.Id)
    }

    return strings.Join(ids, ", ")
}

// Prints a map in the provided order with one key-value-pair per line
func InfoPrint(m map[string]string, keyOrder []string) {
    for _, key := range keyOrder {
        value, ok := m[key]
        if ok && value != "" {
            fmt.Printf("%s: %s\n", key, value)
        }
    }
}

func ISODateToLocal(iso string) string {
    t, err := time.Parse(time.RFC3339, iso)
    if err != nil {
        return iso
    }
    local := t.Local()
    year, month, day := local.Date()
    hour, min, sec := local.Clock()
    return fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", year, month, day, hour, min, sec)
}

func FileSizeFormat(bytes int64) string {
    units := []string{"B", "KB", "MB", "GB", "TB", "PB"}

    var i int
    value := bytes

    for value > 1000 {
        value /= 1000
        i++
    }
    return fmt.Sprintf("%d %s", value, units[i])
}

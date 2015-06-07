// Copyright 2015 Vilibald Wanča. All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package main

import (
    "encoding/json"
    "io/ioutil"
    "os"
    "path"
)

const (
    client_id     = "823682227103-2t1giavsc3umdkk8t72rihdssb047vgf.apps.googleusercontent.com"
    client_secret = "tjeBTD9VxUlIv37PhJaSockg"
    refresh_token = ""
    cfg_file      = ".config.json"
    cache_ttl     = 24
    cache_clean_t = 3600
)

type Config struct {
    ClientId     string `json:"client_id"`
    ClientSecret string `json:"client_secret"`
    RefreshToken string `json:"refresh_token"`
    CacheTTL     int    `json:"cache_ttl"`
    CacheCleanT  int    `json:"cache_clean_t"`
    Path         string `json:"-"`
    DataDir      string `json:"-"`
}

func loadConfig(absPath string) (*Config, error) {
    var data []byte
    var err error
    if data, err = ioutil.ReadFile(absPath); err != nil {
        return nil, err
    }
    c := &Config{}
    err = json.Unmarshal(data, c)
    c.Path = absPath
    return c, err
}

func (c *Config) Write() error {
    var data []byte
    var err error
    if data, err = json.Marshal(c); err != nil {
        return err
    }
    dir := path.Dir(c.Path)
    os.MkdirAll(dir, 0700)
    return ioutil.WriteFile(c.Path, data, 0600)
}

func Initialize(absPath string) (*Config, error) {
    var c *Config
    p := path.Join(absPath, cfg_file)
    _, err := os.Stat(p)
    // No connection file found
    if err != nil {
        c = &Config{client_id, client_secret, refresh_token,
            cache_ttl, cache_clean_t, p, absPath}
        err = nil
    } else {
        c, err = loadConfig(p)
        c.DataDir = absPath
    }
    return c, err
}

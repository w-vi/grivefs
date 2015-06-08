# grivefs

Attemtp to get a google drive [FUSE](http://fuse.sourceforge.net/)
client without huge dependecies. There are not many drive clients for linux and frankly I don't
like syncing clients anyway and the existing fuse clients are either old or have too many dependecies.

## How it works

It creates a mirror of the directory structure from drive in
memory. To access the files it downloads them to the `grivefs` cache
first which is stored by default in `~/.grivefs` directory. This cache
stores the files for 24 hours by default, see *configuration* below
for more details.

## Status

**WIP** so not much to see yet.

But it is already a least a bit usefull. `grivefs` now provides
**READ-ONLY** access to google drive files. No writing, creating or
changing any of the attributes it doesn't update the `LastViewedByMe`
property of the google drive file either.

**Important**: Unfortunatelly it is not yet syncing with the drive if
  you want to see changes on the drive locally you have to restart
  `grivefs`

### Supported platforms

Currently only gnu/linux but it can with little changes run on any FUSE
enabled system. If you want to give it a go just add the system to the
`// +build linux` in `grivefs.go` and possibly add or change the
`atime_OS.go` file for `atime` of a file.

### What's missing?

+ Tests
+ Syncing with the drive
+ Docs
+ Write access
+ More tests

## Usage

### Mounting

`grivefs MOUNTPOINT` where `MOUNTPOINT` is a directory where the
drive should be mounted.

### Unmounting

`fusermount -u MOUNTPOINT`. If you kill `grivefs` or it crashes for
whatever reason you still need to run this command otherwise kernel
will think that there is still something and re-mounting will fail.

### Options

+ `-dir` set the grivefs cache and config directory, default is `~/.grivefs`
+ `-fusedebug` enable fuse ops debugging to stderr
+ `-v` enable debugging messages to stderr

### Configuration

There is a configuration file `.config.json` which is just a JSON
formated text file in the `grivefs` directory. This file is created
after the first run after you authorize `grivefs` to access your
drive and sets default values.

Interesting fields are:
+ cache_ttl - how long in hours the file is kept in the cache, default
  is 24 hours (one day)
+ cache_clean_t - how often the cleaning procedure should be called in
  seconds, default is 3600 (once an hour)

## Acknowledgements

`grivefs` would not be so easy without
[Go FUSE package](https://bazil.org/fuse/) from
[bazil.org/](https://bazil.org/).

## LICENSE

MIT, see LICENSE file for more details.

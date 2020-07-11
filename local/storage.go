// Package local tracks files by writing to JSON files in the dotfile directory.
//
// For every new file that is tracked a new .json file is created.
// For each commit on a tracked file, a new file is created with the same name as the hash.
//
// Example: ~/.emacs.d/init.el is added with alias "emacs".
// Supposing Storage.dir is ~/.config/dotfile, then the following files are created:
//
// ~/.config/dotfile/emacs.json
// ~/.config/dotfile/emacs/8f94c7720a648af9cf9dab33e7f297d28b8bf7cd
//
// The emacs.json file would look something like this:
// {
//   "path": "~/.emacs.d/init.el",
//   "revision": "8f94c7720a648af9cf9dab33e7f297d28b8bf7cd"
//   "commits": [{
//     "hash": "8f94c7720a648af9cf9dab33e7f297d28b8bf7cd",
//     "timestamp": 1558896290,
//     "message": "Initial commit"
//   }]
// }
package local

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/knoebber/dotfile/file"
	"github.com/knoebber/dotfile/usererr"
	"github.com/pkg/errors"
)

// Storage implements the file.Storer interface.
// It provides methods for manipulating tracked files on the file system.
type Storage struct {
	Home     string             // The path to the users home directory.
	Alias    string             // A friendly name for the file that is being tracked.
	FileData *file.TrackingData // The current file that storage is tracking.
	HasFile  bool               // Whether the storage has a TrackedFile loaded.
	User     *UserConfig

	dir      string // The path to the folder where data will be stored.
	jsonPath string
}

// GetJSON returns the tracked files json.
func (s *Storage) GetJSON() ([]byte, error) {
	jsonContent, err := ioutil.ReadFile(s.jsonPath)
	if err != nil {
		return nil, errors.Wrap(err, "reading tracking data")
	}

	return jsonContent, nil
}

// LoadFile sets storage to track alias.
// Loads the tracking data when it exists otherwise sets an empty TrackedFile.
func (s *Storage) LoadFile(alias string) error {
	if alias == "" {
		return errors.New("alias cannot be empty")
	}
	s.Alias = alias
	s.jsonPath = filepath.Join(s.dir, s.Alias+".json")
	s.FileData = new(file.TrackingData)

	if !exists(s.jsonPath) {
		s.FileData.Commits = []file.Commit{}
		s.HasFile = false
		return nil
	}

	jsonContent, err := s.GetJSON()
	if err != nil {
		return nil
	}

	if err = json.Unmarshal(jsonContent, &s.FileData); err != nil {
		return errors.Wrapf(err, "unmarshaling tracking data")
	}

	s.HasFile = true
	return nil
}

// Close updates the files JSON with s.FileData.
func (s *Storage) Close() error {
	bytes, err := json.MarshalIndent(s.FileData, "", jsonIndent)
	if err != nil {
		return errors.Wrap(err, "marshalling tracking data to json")
	}

	// Example: ~/.local/share/dotfile/bash_profile.json
	if err := ioutil.WriteFile(s.jsonPath, bytes, 0644); err != nil {
		return errors.Wrap(err, "saving tracking data")
	}

	return nil
}

// HasCommit return whether the file has a commit with hash.
// This never returns an error; it's present to satisfy a file.Storer requirement.
func (s *Storage) HasCommit(hash string) (exists bool, err error) {
	for _, c := range s.FileData.Commits {
		if c.Hash == hash {
			return true, nil
		}
	}
	return
}

// GetRevision returns the files state at hash.
func (s *Storage) GetRevision(hash string) ([]byte, error) {
	revisionPath := filepath.Join(s.dir, s.Alias, hash)

	bytes, err := ioutil.ReadFile(revisionPath)
	if err != nil {
		return nil, errors.Wrapf(err, "reading revision %#v", hash)
	}

	return bytes, nil
}

// GetContents reads the contents of the file that is being tracked.
func (s *Storage) GetContents() ([]byte, error) {
	contents, err := ioutil.ReadFile(s.GetPath())
	if err != nil {
		return nil, errors.Wrap(err, "reading file contents")
	}

	return contents, nil
}

// SaveCommit saves a commit to the file system.
// Creates a new directory when its the first commit.
// Updates the file's revision field to point to the new hash.
func (s *Storage) SaveCommit(buff *bytes.Buffer, c *file.Commit) error {
	s.FileData.Commits = append(s.FileData.Commits, *c)
	if err := writeCommit(buff.Bytes(), s.dir, s.Alias, c.Hash); err != nil {
		return err
	}

	s.FileData.Revision = c.Hash
	return nil
}

// Revert overwrites a file at path with contents.
func (s *Storage) Revert(buff *bytes.Buffer, hash string) error {
	err := ioutil.WriteFile(s.GetPath(), buff.Bytes(), 0644)
	if err != nil {
		return errors.Wrapf(err, "reverting file %q", s.GetPath())
	}

	s.FileData.Revision = hash
	return nil
}

// GetPath gets the full path to the file.
// Returns an empty string when path is not set.
func (s *Storage) GetPath() string {
	if s.FileData.Path == "" {
		return ""
	}

	if s.FileData.Path[0] == '/' {
		return s.FileData.Path
	}

	return strings.Replace(s.FileData.Path, "~", s.Home, 1)
}

// Push pushes a file's commits to a remote dotfile server.
// Updates the remote file with the new content from local.
func (s *Storage) Push() error {
	var newHashes []string

	client, remoteData, fileURL, err := getRemoteData(s)
	if err != nil {
		return err
	}

	s.FileData, newHashes, err = file.MergeTrackingData(remoteData, s.FileData)
	if err != nil {
		return err
	}

	fmt.Println("pushing", s.FileData.Path)
	if err := postData(s, client, newHashes, fileURL); err != nil {
		return err
	}

	return nil
}

// Pull retrieves a file's commits from a dotfile server.
// Updates the local file with the new content from remote.
// Closes storage.
func (s *Storage) Pull() error {
	var newHashes []string

	client, remoteData, fileURL, err := getRemoteData(s)
	if err != nil {
		return err
	}

	s.FileData, newHashes, err = file.MergeTrackingData(s.FileData, remoteData)
	if err != nil {
		return err
	}

	// If the pulled file is new and a file with the remotes path already exists.
	if !s.HasFile && exists(s.GetPath()) {
		return usererr.Invalid(remoteData.Path +
			" already exists and is not tracked by dotfile. Remove the file or initialize it before pulling")
	}

	fmt.Printf("pulling %d new revisions for %s\n", len(newHashes), s.FileData.Path)

	remoteRevisions, err := getRemoteRevisions(client, fileURL, newHashes)
	if err != nil {
		return err
	}

	for _, rr := range remoteRevisions {
		if err = writeCommit(rr.revision, s.dir, s.Alias, rr.hash); err != nil {
			return err
		}
	}

	// This closes storage.
	return file.Checkout(s, s.FileData.Revision)
}

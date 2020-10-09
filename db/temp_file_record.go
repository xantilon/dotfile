package db

import (
	"database/sql"
	"time"

	"github.com/knoebber/dotfile/dotfile"
	"github.com/pkg/errors"
)

// TempFileRecord models the temp_files table.
// It represents a changed/new file that has not yet been commited.
// Similar to an untracked or dirty file on the filesystem.
// This allows the user to "stage" a file and view results before saving.
// User to TempFile is a one to one relationship.
type TempFileRecord struct {
	ID        int64
	UserID    int64  `validate:"required"`
	Alias     string `validate:"required"`
	Path      string `validate:"required"`
	Content   []byte `validate:"required"`
	CreatedAt time.Time
}

func (*TempFileRecord) createStmt() string {
	return `
CREATE TABLE IF NOT EXISTS temp_files(
id         INTEGER PRIMARY KEY,
user_id    INTEGER NOT NULL REFERENCES users,
alias      TEXT NOT NULL COLLATE NOCASE,
path       TEXT NOT NULL COLLATE NOCASE,
content    BLOB NOT NULL,
created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS temp_files_user_index ON temp_files(user_id);
`
}

func (f *TempFileRecord) check(Executor) error {
	if err := checkFile(f.Alias, f.Path); err != nil {
		return err
	}

	return nil
}

// Inserts or updates a user's previous temp file.
func (f *TempFileRecord) insertStmt(e Executor) (sql.Result, error) {
	compressed, err := dotfile.Compress(f.Content)
	if err != nil {
		return nil, err
	}
	content := compressed.Bytes()

	if err := checkSize(content, "File "+f.Alias); err != nil {
		return nil, err
	}

	return e.Exec(`
INSERT INTO temp_files
(user_id, alias, path, content) 
VALUES
(?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE
SET alias = ?, path = ?, content = ?`,
		f.UserID,
		f.Alias,
		f.Path,
		content,
		f.Alias,
		f.Path,
		content,
	)
}

// Create creates a new temp file.
func (f *TempFileRecord) Create(e Executor) error {
	id, err := insert(e, f)
	if err != nil {
		return err
	}
	f.ID = id

	return nil
}

func (f *TempFileRecord) save(e Executor) (newFileID int64, err error) {
	newFile := &FileRecord{
		UserID: f.UserID,
		Alias:  f.Alias,
		Path:   f.Path,
	}

	newFileID, err = insert(e, newFile)
	if err != nil {
		return 0, err
	}

	_, err = e.Exec("DELETE FROM temp_files WHERE user_id = ?", f.UserID)
	if err != nil {
		return 0, errors.Wrapf(err, "deleting temp file %q for user %d", f.Alias, f.UserID)
	}

	return
}

// DeleteTempFile deletes a users temp file.
func DeleteTempFile(e Executor, username string) error {
	_, err := e.Exec(`
DELETE FROM temp_files WHERE user_id = (SELECT id FROM users WHERE username = ?)`, username)
	if err != nil {
		return errors.Wrapf(err, "deleting %q temp file", username)
	}
	return nil

}

// TempFile finds a user's temp file.
// Users can only have one temp file at a time so alias can be empty.
// When alias is present, ensures that temp file exists with alias.
func TempFile(e Executor, username string, alias string) (*TempFileRecord, error) {
	res := new(TempFileRecord)

	if err := e.
		QueryRow(`
SELECT temp_files.* 
FROM temp_files 
JOIN users ON user_id = users.id
WHERE username = ? AND (? = '' OR alias = ?)`, username, alias, alias).
		Scan(
			&res.ID,
			&res.UserID,
			&res.Alias,
			&res.Path,
			&res.Content,
			&res.CreatedAt,
		); err != nil {
		return nil, errors.Wrapf(err, "querying for user %q temp file %q", username, alias)
	}

	buff, err := dotfile.Uncompress(res.Content)
	if err != nil {
		return nil, err
	}

	res.Content = buff.Bytes()

	return res, nil
}

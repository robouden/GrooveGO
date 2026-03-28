package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// Store wraps a Badger key-value database for local message persistence.
type Store struct {
	db       *badger.DB
	filesDir string
}

// Message mirrors transport.Message — duplicated here to avoid a circular dep.
// Fields must stay in sync with transport.Message.
type Message struct {
	From      string    `json:"from"`
	Workspace string    `json:"workspace"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"ts"`
	// File fields (omitted when empty)
	MsgType  string `json:"msg_type,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	FileName string `json:"file_name,omitempty"`
	FileMime string `json:"file_mime,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
	FileData string `json:"file_data,omitempty"`
}

// Open opens (or creates) a Badger database at the given directory path.
func Open(dir string) (*Store, error) {
	opts := badger.DefaultOptions(dir).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("store open: %w", err)
	}
	filesDir := filepath.Join(dir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, fmt.Errorf("store files dir: %w", err)
	}
	fmt.Printf("[store] opened at %s\n", dir)
	return &Store{db: db, filesDir: filesDir}, nil
}

// Close flushes and closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Save persists a message (metadata only — FileData should be stripped before calling).
// Key format: msg/<workspace>/<unix-nano>/<from>
func (s *Store) Save(msg Message) error {
	key := fmt.Sprintf("msg/%s/%d/%s", msg.Workspace, msg.Timestamp.UnixNano(), msg.From)
	val, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), val)
	})
}

// History returns all stored messages for a workspace, oldest first.
func (s *Store) History(workspace string) ([]Message, error) {
	prefix := []byte("msg/" + workspace + "/")
	var msgs []Message

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if err := item.Value(func(v []byte) error {
				var m Message
				if err := json.Unmarshal(v, &m); err != nil {
					return err
				}
				msgs = append(msgs, m)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return msgs, err
}

// SaveFile writes raw file bytes to the files directory.
// Returns the path the file was saved to.
func (s *Store) SaveFile(fileID, fileName string, data []byte) (string, error) {
	// Sanitise filename — keep only the base name
	safe := filepath.Base(fileName)
	path := filepath.Join(s.filesDir, fileID+"_"+safe)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("save file: %w", err)
	}
	return path, nil
}

// FilePath returns the on-disk path for a previously saved file.
func (s *Store) FilePath(fileID, fileName string) string {
	safe := filepath.Base(fileName)
	return filepath.Join(s.filesDir, fileID+"_"+safe)
}

// FilesDir returns the directory where received files are stored.
func (s *Store) FilesDir() string {
	return s.filesDir
}

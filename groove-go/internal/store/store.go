package store

import (
	"encoding/json"
	"fmt"
	"time"

	badger "github.com/dgraph-io/badger/v4"
)

// Store wraps a Badger key-value database for local message persistence.
type Store struct {
	db *badger.DB
}

// Message mirrors transport.Message — duplicated here to avoid a circular dep.
type Message struct {
	From      string    `json:"from"`
	Workspace string    `json:"workspace"`
	Body      string    `json:"body"`
	Timestamp time.Time `json:"ts"`
}

// Open opens (or creates) a Badger database at the given directory path.
func Open(dir string) (*Store, error) {
	opts := badger.DefaultOptions(dir).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("store open: %w", err)
	}
	fmt.Printf("[store] opened at %s\n", dir)
	return &Store{db: db}, nil
}

// Close flushes and closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Save persists a message. Key format: msg/<workspace>/<unix-nano>/<from>
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

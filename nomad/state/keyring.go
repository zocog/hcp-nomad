package state

import (
	"fmt"

	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/nomad/structs"
)

// UpsertWrappedRootKeys saves root key meta or updates it in-place.
func (s *StateStore) UpsertWrappedRootKeys(index uint64, wrappedRootKeys *structs.WrappedRootKeys, rekey bool) error {
	txn := s.db.WriteTxn(index)
	defer txn.Abort()

	// get any existing key for updating
	raw, err := txn.First(TableWrappedRootKeys, indexID, wrappedRootKeys.Meta.KeyID)
	if err != nil {
		return fmt.Errorf("root key lookup failed: %v", err)
	}

	isRotation := false

	if raw != nil {
		existing := raw.(*structs.RootKeyMeta)
		wrappedRootKeys.Meta.CreateIndex = existing.CreateIndex
		wrappedRootKeys.Meta.CreateTime = existing.CreateTime
		isRotation = !existing.IsActive() && wrappedRootKeys.Meta.IsActive()
	} else {
		wrappedRootKeys.Meta.CreateIndex = index
		isRotation = wrappedRootKeys.Meta.IsActive()
	}
	wrappedRootKeys.Meta.ModifyIndex = index

	if rekey && !isRotation {
		return fmt.Errorf("cannot rekey without setting the new key active")
	}

	// if the upsert is for a newly-active key, we need to set all the
	// other keys as inactive in the same transaction.
	if isRotation {
		iter, err := txn.Get(TableWrappedRootKeys, indexID)
		if err != nil {
			return err
		}
		for {
			raw := iter.Next()
			if raw == nil {
				break
			}
			key := raw.(*structs.WrappedRootKeys)
			modified := false

			switch key.Meta.State {
			case structs.RootKeyStateInactive:
				if rekey {
					key.Meta = key.Meta.MakeRekeying()
					modified = true
				}
			case structs.RootKeyStateActive:
				if rekey {
					key.Meta = key.Meta.MakeRekeying()
				} else {
					key.Meta = key.Meta.MakeInactive()
				}
				modified = true
			case structs.RootKeyStateRekeying, structs.RootKeyStateDeprecated:
				// nothing to do
			}

			if modified {
				key.Meta.ModifyIndex = index
				if err := txn.Insert(TableWrappedRootKeys, key); err != nil {
					return err
				}
			}

		}
	}

	if err := txn.Insert(TableWrappedRootKeys, wrappedRootKeys); err != nil {
		return err
	}

	// update the indexes table
	if err := txn.Insert("index", &IndexEntry{TableWrappedRootKeys, index}); err != nil {
		return fmt.Errorf("index update failed: %v", err)
	}
	return txn.Commit()
}

// DeleteWrappedRootKeys deletes a single root key, or returns an error if
// it doesn't exist.
func (s *StateStore) DeleteWrappedRootKeys(index uint64, keyID string) error {
	txn := s.db.WriteTxn(index)
	defer txn.Abort()

	// find the old key
	existing, err := txn.First(TableWrappedRootKeys, indexID, keyID)
	if err != nil {
		return fmt.Errorf("root key lookup failed: %v", err)
	}
	if existing == nil {
		return fmt.Errorf("root key not found")
	}
	if err := txn.Delete(TableWrappedRootKeys, existing); err != nil {
		return fmt.Errorf("root key delete failed: %v", err)
	}

	if err := txn.Insert("index", &IndexEntry{TableWrappedRootKeys, index}); err != nil {
		return fmt.Errorf("index update failed: %v", err)
	}

	return txn.Commit()
}

// WrappedRootKeys returns an iterator over all wrapped root key
func (s *StateStore) WrappedRootKeys(ws memdb.WatchSet) (memdb.ResultIterator, error) {
	txn := s.db.ReadTxn()

	iter, err := txn.Get(TableWrappedRootKeys, indexID)
	if err != nil {
		return nil, err
	}

	ws.Add(iter.WatchCh())
	return iter, nil
}

// WrappedRootKeysByID returns a specific wrapped root key
func (s *StateStore) WrappedRootKeysByID(ws memdb.WatchSet, id string) (*structs.WrappedRootKeys, error) {
	txn := s.db.ReadTxn()

	watchCh, raw, err := txn.FirstWatch(TableWrappedRootKeys, indexID, id)
	if err != nil {
		return nil, fmt.Errorf("root key metadata lookup failed: %v", err)
	}
	ws.Add(watchCh)

	if raw != nil {
		return raw.(*structs.WrappedRootKeys), nil
	}
	return nil, nil
}

// GetActiveWrappedRootKeys returns the currently active wrapped root key
func (s *StateStore) GetActiveWrappedRootKeys(ws memdb.WatchSet) (*structs.WrappedRootKeys, error) {
	txn := s.db.ReadTxn()

	iter, err := txn.Get(TableWrappedRootKeys, indexID)
	if err != nil {
		return nil, err
	}
	ws.Add(iter.WatchCh())

	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.WrappedRootKeys)
		if key.Meta.IsActive() {
			return key, nil
		}
	}
	return nil, nil
}

// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package state

import (
	"fmt"

	"github.com/davecgh/go-spew/spew"
	memdb "github.com/hashicorp/go-memdb"
	"github.com/hashicorp/nomad/nomad/structs"
)

// UpsertRootKeyMeta saves root key meta or updates it in-place.
//
// COMPAT(1.12.0): remove in 1.12.0 LTS
func (s *StateStore) UpsertRootKeyMeta(index uint64, rootKeyMeta *structs.RootKeyMeta, rekey bool) error {
	txn := s.db.WriteTxn(index)
	defer txn.Abort()

	// get any existing key for updating
	raw, err := txn.First(TableRootKeyMeta, indexID, rootKeyMeta.KeyID)
	if err != nil {
		return fmt.Errorf("root key metadata lookup failed: %v", err)
	}

	isRotation := false

	if raw != nil {
		existing := raw.(*structs.RootKeyMeta)
		rootKeyMeta.CreateIndex = existing.CreateIndex
		rootKeyMeta.CreateTime = existing.CreateTime
		isRotation = !existing.IsActive() && rootKeyMeta.IsActive()
	} else {
		rootKeyMeta.CreateIndex = index
		isRotation = rootKeyMeta.IsActive()
	}
	rootKeyMeta.ModifyIndex = index

	if rekey && !isRotation {
		return fmt.Errorf("cannot rekey without setting the new key active")
	}

	// if the upsert is for a newly-active key, we need to set all the
	// other keys as inactive in the same transaction.
	if isRotation {
		iter, err := txn.Get(TableRootKeyMeta, indexID)
		if err != nil {
			return err
		}
		for {
			raw := iter.Next()
			if raw == nil {
				break
			}
			key := raw.(*structs.RootKeyMeta)
			modified := false

			switch key.State {
			case structs.RootKeyStateInactive:
				if rekey {
					key = key.MakeRekeying()
					modified = true
				}
			case structs.RootKeyStateActive:
				if rekey {
					key = key.MakeRekeying()
				} else {
					key = key.MakeInactive()
				}
				modified = true
			case structs.RootKeyStateRekeying, structs.RootKeyStateDeprecated:
				// nothing to do
			}

			if modified {
				key.ModifyIndex = index
				if err := txn.Insert(TableRootKeyMeta, key); err != nil {
					return err
				}
			}

		}
	}

	if err := txn.Insert(TableRootKeyMeta, rootKeyMeta); err != nil {
		return err
	}

	// update the indexes table
	if err := txn.Insert("index", &IndexEntry{TableRootKeyMeta, index}); err != nil {
		return fmt.Errorf("index update failed: %v", err)
	}
	return txn.Commit()
}

// DeleteRootKeyMeta deletes a single root key, or returns an error if
// it doesn't exist.
//
// COMPAT(1.12.0): remove in 1.12.0 LTS
func (s *StateStore) DeleteRootKeyMeta(index uint64, keyID string) error {
	txn := s.db.WriteTxn(index)
	defer txn.Abort()
	if err := s.deleteRootKeyMetaTxn(txn, index, keyID); err != nil {
		return err
	}
	return txn.Commit()
}

func (s *StateStore) deleteRootKeyMetaTxn(txn Txn, index uint64, keyID string) error {
	existing, err := txn.First(TableRootKeyMeta, indexID, keyID)
	if err != nil {
		return fmt.Errorf("root key metadata lookup failed: %v", err)
	}
	if existing == nil {
		return nil // validated by RPC or ignored in FSM
	}
	if err := txn.Delete(TableRootKeyMeta, existing); err != nil {
		return fmt.Errorf("root key metadata delete failed: %v", err)
	}

	// update the indexes table
	if err := txn.Insert("index", &IndexEntry{TableRootKeyMeta, index}); err != nil {
		return fmt.Errorf("index update failed: %v", err)
	}

	return nil
}

// RootKeyMetas returns an iterator over all root key metadata
//
// COMPAT(1.12.0): remove in 1.12.0 LTS
func (s *StateStore) RootKeyMetas(ws memdb.WatchSet) (memdb.ResultIterator, error) {
	txn := s.db.ReadTxn()

	iter, err := txn.Get(TableRootKeyMeta, indexID)
	if err != nil {
		return nil, err
	}

	ws.Add(iter.WatchCh())
	return iter, nil
}

// RootKeyMetaByID returns a specific root key meta
//
// COMPAT(1.12.0): remove in 1.12.0 LTS
func (s *StateStore) RootKeyMetaByID(ws memdb.WatchSet, id string) (*structs.RootKeyMeta, error) {
	txn := s.db.ReadTxn()

	watchCh, raw, err := txn.FirstWatch(TableRootKeyMeta, indexID, id)
	if err != nil {
		return nil, fmt.Errorf("root key metadata lookup failed: %v", err)
	}
	ws.Add(watchCh)

	if raw != nil {
		return raw.(*structs.RootKeyMeta), nil
	}
	return nil, nil
}

// GetActiveRootKeyMeta returns the metadata for the currently active root key
func (s *StateStore) GetActiveRootKeyMeta(ws memdb.WatchSet) (*structs.RootKeyMeta, error) {
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
		wrappedKey := raw.(*structs.WrappedRootKeys)
		spew.Dump(wrappedKey.Meta)
		if wrappedKey.Meta.IsActive() {
			return wrappedKey.Meta, nil
		}
	}

	// look in the legacy table if we can't find it
	// COMPAT(1.12.0): remove this in 1.12.0 LTS
	iter, err = txn.Get(TableRootKeyMeta, indexID)
	if err != nil {
		return nil, err
	}
	ws.Add(iter.WatchCh())

	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.RootKeyMeta)
		spew.Dump(key)
		if key.IsActive() {
			return key, nil
		}
	}
	return nil, nil
}

// IsRootKeyInUse determines whether a key has been used to sign a workload
// identity for a live allocation or encrypt any variables
func (s *StateStore) IsRootKeyInUse(keyID string) (bool, error) {
	txn := s.db.ReadTxn()

	iter, err := txn.Get(TableAllocs, indexSigningKey, keyID, true)
	if err != nil {
		return false, err
	}
	alloc := iter.Next()
	if alloc != nil {
		return true, nil
	}

	iter, err = txn.Get(TableVariables, indexKeyID, keyID)
	if err != nil {
		return false, err
	}
	variable := iter.Next()
	if variable != nil {
		return true, nil
	}

	return false, nil
}

// UpsertWrappedRootKeys saves a wrapped root keys or updates them in place.
func (s *StateStore) UpsertWrappedRootKeys(index uint64, wrappedRootKeys *structs.WrappedRootKeys, rekey bool) error {
	txn := s.db.WriteTxn(index)
	defer txn.Abort()

	if wrappedRootKeys.Meta == nil {
		return fmt.Errorf("root key must always have meta defined")
	}

	// get any existing key for updating
	raw, err := txn.First(TableWrappedRootKeys, indexID, wrappedRootKeys.Meta.KeyID)
	if err != nil {
		return fmt.Errorf("root key lookup failed: %v", err)
	}

	isRotation := false

	if raw != nil {
		existing := raw.(*structs.WrappedRootKeys)
		wrappedRootKeys.Meta.CreateIndex = existing.Meta.CreateIndex
		wrappedRootKeys.Meta.CreateTime = existing.Meta.CreateTime
		isRotation = !existing.Meta.IsActive() && wrappedRootKeys.Meta.IsActive()
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

				// COMPAT(1.12.0): remove this section in 1.12.0 LTS
				if err := s.deleteRootKeyMetaTxn(txn, index, key.Meta.KeyID); err != nil {
					return err
				}

			}
		}

		// // COMPAT(1.12.0): remove this section in 1.12.0 LTS
		// var legacyTableUpdated bool
		// iter, err = txn.Get(TableRootKeyMeta, indexID)
		// if err != nil {
		// 	return err
		// }
		// for {
		// 	raw := iter.Next()
		// 	if raw == nil {
		// 		break
		// 	}
		// 	key := raw.(*structs.RootKeyMeta)
		// 	modified := false

		// 	switch key.State {
		// 	case structs.RootKeyStateInactive:
		// 		if rekey {
		// 			key = key.MakeRekeying()
		// 			modified = true
		// 		}
		// 	case structs.RootKeyStateActive:
		// 		if rekey {
		// 			key = key.MakeRekeying()
		// 		} else {
		// 			key = key.MakeInactive()
		// 		}
		// 		modified = true
		// 	case structs.RootKeyStateRekeying, structs.RootKeyStateDeprecated:
		// 		// nothing to do
		// 	}

		// 	if modified {
		// 		key.ModifyIndex = index
		// 		legacyTableUpdated = true
		// 		if err := txn.Insert(TableRootKeyMeta, key); err != nil {
		// 			return err
		// 		}
		// 	}
		// }
		// if legacyTableUpdated {
		// 	if err := txn.Insert("index", &IndexEntry{TableRootKeyMeta, index}); err != nil {
		// 		return fmt.Errorf("index update failed: %v", err)
		// 	}
		// }

	}

	if err := txn.Insert(TableWrappedRootKeys, wrappedRootKeys); err != nil {
		return err
	}
	if err := txn.Insert("index", &IndexEntry{TableWrappedRootKeys, index}); err != nil {
		return fmt.Errorf("index update failed: %v", err)
	}

	// COMPAT(1.12.0): remove this section in 1.12.0 LTS
	if err := s.deleteRootKeyMetaTxn(txn, index, wrappedRootKeys.Meta.KeyID); err != nil {
		return err
	}

	return txn.Commit()
}

// DeleteWrappedRootKeys deletes a single wrapped root key set, or returns an
// error if it doesn't exist.
func (s *StateStore) DeleteWrappedRootKeys(index uint64, keyID string) error {
	txn := s.db.WriteTxn(index)
	defer txn.Abort()

	// find the old key
	existing, err := txn.First(TableWrappedRootKeys, indexID, keyID)
	if err != nil {
		return fmt.Errorf("root key lookup failed: %v", err)
	}
	if existing == nil {
		return nil // this case should be validated in RPC
	}
	if err := txn.Delete(TableWrappedRootKeys, existing); err != nil {
		return fmt.Errorf("root key delete failed: %v", err)
	}

	if err := txn.Insert("index", &IndexEntry{TableWrappedRootKeys, index}); err != nil {
		return fmt.Errorf("index update failed: %v", err)
	}

	return txn.Commit()
}

// WrappedRootKeys returns an iterator over all wrapped root keys
func (s *StateStore) WrappedRootKeys(ws memdb.WatchSet) (memdb.ResultIterator, error) {
	txn := s.db.ReadTxn()

	iter, err := txn.Get(TableWrappedRootKeys, indexID)
	if err != nil {
		return nil, err
	}

	ws.Add(iter.WatchCh())
	return iter, nil
}

// WrappedRootKeysByID returns a specific wrapped root key set
func (s *StateStore) WrappedRootKeysByID(ws memdb.WatchSet, id string) (*structs.WrappedRootKeys, error) {
	txn := s.db.ReadTxn()

	watchCh, raw, err := txn.FirstWatch(TableWrappedRootKeys, indexID, id)
	if err != nil {
		return nil, fmt.Errorf("root key lookup failed: %v", err)
	}
	ws.Add(watchCh)

	if raw != nil {
		return raw.(*structs.WrappedRootKeys), nil
	}
	return nil, nil
}

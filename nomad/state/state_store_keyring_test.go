// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package state

import (
	"testing"

	"github.com/hashicorp/nomad/ci"
	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/shoenig/test/must"
)

func TestStateStore_RootKeyMetaData_CRUD(t *testing.T) {
	ci.Parallel(t)
	store := testStateStore(t)
	index, err := store.LatestIndex()
	must.NoError(t, err)

	// create 3 default keys, one of which is active
	keyIDs := []string{}
	for i := 0; i < 3; i++ {
		key := structs.NewRootKeyMeta()
		keyIDs = append(keyIDs, key.KeyID)
		if i == 0 {
			key = key.MakeActive()
		}
		index++
		must.NoError(t, store.UpsertRootKeyMeta(index, key, false))
	}

	// retrieve the active key
	activeKey, err := store.GetActiveRootKeyMeta(nil)
	must.NoError(t, err)
	must.NotNil(t, activeKey)

	// update an inactive key to active and verify the rotation
	inactiveKey, err := store.RootKeyMetaByID(nil, keyIDs[1])
	must.NoError(t, err)
	must.NotNil(t, inactiveKey)
	oldCreateIndex := inactiveKey.CreateIndex
	newlyActiveKey := inactiveKey.MakeActive()
	index++
	must.NoError(t, store.UpsertRootKeyMeta(index, newlyActiveKey, false))

	iter, err := store.RootKeyMetas(nil)
	must.NoError(t, err)
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.RootKeyMeta)
		if key.KeyID == newlyActiveKey.KeyID {
			must.True(t, key.IsActive(), must.Sprint("expected updated key to be active"))
			must.Eq(t, oldCreateIndex, key.CreateIndex)
		} else {
			must.False(t, key.IsActive(), must.Sprint("expected other keys to be inactive"))
		}
	}

	// delete the active key and verify it's been deleted
	index++
	must.NoError(t, store.DeleteRootKeyMeta(index, keyIDs[1]))

	iter, err = store.RootKeyMetas(nil)
	must.NoError(t, err)
	var found int
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.RootKeyMeta)
		must.NotEq(t, keyIDs[1], key.KeyID)
		must.False(t, key.IsActive(), must.Sprint("expected remaining keys to be inactive"))
		found++
	}
	must.Eq(t, 2, found, must.Sprint("expected only 2 keys remaining"))

	// deleting non-existent keys is safe
	must.NoError(t, store.DeleteRootKeyMeta(index, uuid.Generate()))
}

func TestStateStore_WrappedRootKey_CRUD(t *testing.T) {
	ci.Parallel(t)
	store := testStateStore(t)
	index, err := store.LatestIndex()
	must.NoError(t, err)

	// create 3 default keys, one of which is active
	keyIDs := []string{}
	for i := 0; i < 3; i++ {
		key := structs.NewRootKeyMeta()
		keyIDs = append(keyIDs, key.KeyID)
		if i == 0 {
			key = key.MakeActive()
		}
		index++
		wrappedKeys := structs.NewWrappedRootKeys(key)
		must.NoError(t, store.UpsertWrappedRootKeys(index, wrappedKeys, false))
	}

	// retrieve the active key
	activeKey, err := store.GetActiveRootKeyMeta(nil)
	must.NoError(t, err)
	must.NotNil(t, activeKey)

	// update an inactive key to active and verify the rotation
	inactiveKey, err := store.WrappedRootKeysByID(nil, keyIDs[1])
	must.NoError(t, err)
	must.NotNil(t, inactiveKey)
	oldCreateIndex := inactiveKey.Meta.CreateIndex
	newlyActiveKey := inactiveKey.Copy()
	newlyActiveKey.Meta = inactiveKey.Meta.MakeActive()
	index++
	must.NoError(t, store.UpsertWrappedRootKeys(index, newlyActiveKey, false))

	iter, err := store.WrappedRootKeys(nil)
	must.NoError(t, err)
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.WrappedRootKeys)
		if key.Meta.KeyID == newlyActiveKey.Meta.KeyID {
			must.True(t, key.Meta.IsActive(), must.Sprint("expected updated key to be active"))
			must.Eq(t, oldCreateIndex, key.Meta.CreateIndex)
		} else {
			must.False(t, key.Meta.IsActive(), must.Sprint("expected other keys to be inactive"))
		}
	}

	// delete the active key and verify it's been deleted
	index++
	must.NoError(t, store.DeleteWrappedRootKeys(index, keyIDs[1]))

	iter, err = store.WrappedRootKeys(nil)
	must.NoError(t, err)
	var found int
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.WrappedRootKeys)
		must.NotEq(t, keyIDs[1], key.Meta.KeyID)
		must.False(t, key.Meta.IsActive(), must.Sprint("expected remaining keys to be inactive"))
		found++
	}
	must.Eq(t, 2, found, must.Sprint("expected only 2 keys remaining"))

	// deleting non-existent keys is safe
	must.NoError(t, store.DeleteWrappedRootKeys(index, uuid.Generate()))
}

// TestStateStore_WrappedRootKey_Migration tests logic around migrating keys
// from the legacy table to the new table.
//
// COMPAT(1.12.0) - remove in 1.12.0 LTS
func TestStateStore_WrappedRootKey_Migration(t *testing.T) {
	ci.Parallel(t)
	store := testStateStore(t)
	index, err := store.LatestIndex()
	must.NoError(t, err)

	// create 3 default keys, one of which is active
	expectKeyIDs := []string{}
	keyMetas := []*structs.RootKeyMeta{}
	for i := 0; i < 3; i++ {
		keyMeta := structs.NewRootKeyMeta()
		expectKeyIDs = append(expectKeyIDs, keyMeta.KeyID)
		if i == 0 {
			keyMeta = keyMeta.MakeActive()
		}
		keyMetas = append(keyMetas, keyMeta)
		index++
		must.NoError(t, store.UpsertRootKeyMeta(index, keyMeta, false))
	}

	// create 3 default keys, one of which is active
	for _, keyMeta := range keyMetas {
		wrappedKeys := structs.NewWrappedRootKeys(keyMeta)
		must.NoError(t, store.UpsertWrappedRootKeys(index, wrappedKeys, false))
	}

	// retrieve the active key
	activeKey, err := store.GetActiveRootKeyMeta(nil)
	must.NoError(t, err)
	must.NotNil(t, activeKey)

	// verify there are no RootKeyMeta anymore
	iter, err := store.RootKeyMetas(nil)
	must.NoError(t, err)
	must.Nil(t, iter.Next(), must.Sprint("expected legacy table to be empty"))

	keyIDs := []string{}
	iter, err = store.WrappedRootKeys(nil)
	must.NoError(t, err)
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		key := raw.(*structs.WrappedRootKeys)
		keyIDs = append(keyIDs, key.Meta.KeyID)
	}

	must.SliceContainsAll(t, expectKeyIDs, keyIDs)
}

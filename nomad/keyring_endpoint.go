// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package nomad

import (
	"fmt"
	"time"

	"github.com/armon/go-metrics"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-memdb"

	"github.com/hashicorp/nomad/helper/uuid"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
)

// Keyring endpoint serves RPCs for root key management
type Keyring struct {
	srv    *Server
	ctx    *RPCContext
	logger hclog.Logger

	encrypter *Encrypter
}

func NewKeyringEndpoint(srv *Server, ctx *RPCContext, enc *Encrypter) *Keyring {
	return &Keyring{srv: srv, ctx: ctx, logger: srv.logger.Named("keyring"), encrypter: enc}
}

func (k *Keyring) Rotate(args *structs.KeyringRotateRootKeyRequest, reply *structs.KeyringRotateRootKeyResponse) error {

	authErr := k.srv.Authenticate(k.ctx, args)
	if done, err := k.srv.forward("Keyring.Rotate", args, args, reply); done {
		return err
	}
	k.srv.MeasureRPCRate("keyring", structs.RateMetricWrite, args)
	if authErr != nil {
		return structs.ErrPermissionDenied
	}
	defer metrics.MeasureSince([]string{"nomad", "keyring", "rotate"}, time.Now())

	if aclObj, err := k.srv.ResolveACL(args); err != nil {
		return err
	} else if !aclObj.IsManagement() {
		return structs.ErrPermissionDenied
	}

	if args.Algorithm == "" {
		args.Algorithm = structs.EncryptionAlgorithmAES256GCM
	}
	if args.Full && args.PublishTime > 0 {
		return fmt.Errorf("keyring cannot be prepublished and full rotated at the same time")
	}

	rootKey, err := structs.NewRootKey(args.Algorithm)
	if err != nil {
		return err
	}

	if args.PublishTime != 0 {
		rootKey.Meta = rootKey.Meta.MakePrepublished(args.PublishTime)
	} else {
		rootKey.Meta = rootKey.Meta.MakeActive()
	}

	isClusterUpgraded := ServersMeetMinimumVersion(
		k.srv.serf.Members(), k.srv.Region(), minVersionKeyringInRaft, true)

	// wrap/encrypt the key before we write it to Raft. When
	// store_keys_outside_raft=true, this ensures the followers don't try to Get
	// a key that hasn't yet been written to disk
	wrappedKeys, err := k.encrypter.AddUnwrappedKey(rootKey, isClusterUpgraded)
	if err != nil {
		return err
	}

	var index uint64
	if isClusterUpgraded {
		_, index, err = k.srv.raftApply(structs.WrappedRootKeysUpsertRequestType,
			structs.KeyringUpsertWrappedRootKeyRequest{
				WrappedRootKeys: wrappedKeys,
				Rekey:           args.Full,
				WriteRequest:    args.WriteRequest,
			})
	} else {
		// COMPAT(1.12.0): remove the version check and this code path
		_, index, err = k.srv.raftApply(structs.RootKeyMetaUpsertRequestType,
			structs.KeyringUpdateRootKeyMetaRequest{
				RootKeyMeta:  rootKey.Meta,
				Rekey:        args.Full,
				WriteRequest: args.WriteRequest,
			})
	}

	reply.Key = rootKey.Meta
	reply.Index = index

	if args.Full {
		// like most core jobs, we don't commit this to raft b/c it's not
		// going to be periodically recreated and the ACL is from this leader
		eval := &structs.Evaluation{
			ID:          uuid.Generate(),
			Namespace:   "-",
			Priority:    structs.CoreJobPriority,
			Type:        structs.JobTypeCore,
			TriggeredBy: structs.EvalTriggerJobRegister,
			JobID:       structs.CoreJobVariablesRekey,
			Status:      structs.EvalStatusPending,
			ModifyIndex: index,
			LeaderACL:   k.srv.getLeaderAcl(),
		}
		k.srv.evalBroker.Enqueue(eval)
	}

	return nil
}

func (k *Keyring) List(args *structs.KeyringListRootKeyMetaRequest, reply *structs.KeyringListRootKeyMetaResponse) error {

	authErr := k.srv.Authenticate(k.ctx, args)
	if done, err := k.srv.forward("Keyring.List", args, args, reply); done {
		return err
	}
	k.srv.MeasureRPCRate("keyring", structs.RateMetricList, args)
	if authErr != nil {
		return structs.ErrPermissionDenied
	}

	defer metrics.MeasureSince([]string{"nomad", "keyring", "list"}, time.Now())

	if aclObj, err := k.srv.ResolveACL(args); err != nil {
		return err
	} else if !aclObj.IsManagement() {
		return structs.ErrPermissionDenied
	}

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, store *state.StateStore) error {
			keys, index, err := k.listAllKeyMetadata(ws, store)
			if err != nil {
				return err
			}

			// COMPAT(1.12.0): once the RootKeyMeta query is removed we can call
			// replySetIndex here
			reply.Keys = keys
			reply.Index = index
			k.srv.setQueryMeta(&reply.QueryMeta)
			return nil
		},
	}
	return k.srv.blockingRPC(&opts)
}

// Update updates an existing key in the keyring, including both the
// key material and metadata.
func (k *Keyring) Update(args *structs.KeyringUpdateRootKeyRequest, reply *structs.KeyringUpdateRootKeyResponse) error {

	authErr := k.srv.Authenticate(k.ctx, args)
	if done, err := k.srv.forward("Keyring.Update", args, args, reply); done {
		return err
	}
	k.srv.MeasureRPCRate("keyring", structs.RateMetricWrite, args)
	if authErr != nil {
		return structs.ErrPermissionDenied
	}

	defer metrics.MeasureSince([]string{"nomad", "keyring", "update"}, time.Now())

	if aclObj, err := k.srv.ResolveACL(args); err != nil {
		return err
	} else if !aclObj.IsManagement() {
		return structs.ErrPermissionDenied
	}

	err := k.validateUpdate(args)
	if err != nil {
		return err
	}

	isClusterUpgraded := ServersMeetMinimumVersion(
		k.srv.serf.Members(), k.srv.Region(), minVersionKeyringInRaft, true)

	// make sure it's been added to the local keystore before we write
	// it to raft, so that followers don't try to Get a key that
	// hasn't yet been written to disk
	wrappedKeys, err := k.encrypter.AddUnwrappedKey(args.RootKey, isClusterUpgraded)
	if err != nil {
		return err
	}

	var index uint64
	if isClusterUpgraded {
		_, index, err = k.srv.raftApply(structs.WrappedRootKeysUpsertRequestType,
			structs.KeyringUpsertWrappedRootKeyRequest{
				WrappedRootKeys: wrappedKeys,
				WriteRequest:    args.WriteRequest,
			})
	} else {
		// COMPAT(1.12.0): remove the version check and this code path
		// unwrap the request to turn it into a meta update only
		metaReq := &structs.KeyringUpdateRootKeyMetaRequest{
			RootKeyMeta:  args.RootKey.Meta,
			WriteRequest: args.WriteRequest,
		}

		// update the metadata via Raft
		_, index, err = k.srv.raftApply(structs.RootKeyMetaUpsertRequestType, metaReq)
		if err != nil {
			return err
		}
	}

	reply.Index = index
	return nil
}

// validateUpdate validates both the request and that any change to an
// existing key is valid
func (k *Keyring) validateUpdate(args *structs.KeyringUpdateRootKeyRequest) error {

	err := args.RootKey.Meta.Validate()
	if err != nil {
		return err
	}
	if len(args.RootKey.Key) == 0 {
		return fmt.Errorf("root key material is required")
	}

	// lookup any existing key and validate the update
	snap, err := k.srv.fsm.State().Snapshot()
	if err != nil {
		return err
	}
	ws := memdb.NewWatchSet()
	keyMeta, err := snap.RootKeyMetaByID(ws, args.RootKey.Meta.KeyID)
	if err != nil {
		return err
	}
	if keyMeta != nil && keyMeta.Algorithm != args.RootKey.Meta.Algorithm {
		return fmt.Errorf("root key algorithm cannot be changed after a key is created")
	}

	return nil
}

// Get retrieves an existing key from the keyring, including both the
// key material and metadata. It is used only for replication.
func (k *Keyring) Get(args *structs.KeyringGetRootKeyRequest, reply *structs.KeyringGetRootKeyResponse) error {
	aclObj, err := k.srv.AuthenticateServerOnly(k.ctx, args)
	k.srv.MeasureRPCRate("keyring", structs.RateMetricRead, args)

	if err != nil || !aclObj.AllowServerOp() {
		return structs.ErrPermissionDenied
	}

	if done, err := k.srv.forward("Keyring.Get", args, args, reply); done {
		return err
	}
	defer metrics.MeasureSince([]string{"nomad", "keyring", "get"}, time.Now())

	if args.KeyID == "" {
		return fmt.Errorf("root key ID is required")
	}

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, s *state.StateStore) error {

			// retrieve the key metadata
			snap, err := k.srv.fsm.State().Snapshot()
			if err != nil {
				return err
			}
			var keyMeta *structs.RootKeyMeta
			wrappedKeys, err := snap.WrappedRootKeysByID(ws, args.KeyID)
			if err != nil {
				return err
			}
			if wrappedKeys == nil {
				keyMeta, err = snap.RootKeyMetaByID(ws, args.KeyID)
				if err != nil {
					return err
				}
				if keyMeta == nil {
					return k.srv.replySetIndex(state.TableRootKeyMeta, &reply.QueryMeta)
				}
			} else {
				keyMeta = wrappedKeys.Meta
			}

			// retrieve the key material from the keyring
			rootKey, err := k.encrypter.GetKey(keyMeta.KeyID)
			if err != nil {
				return err
			}
			reply.Key = rootKey

			// Use the last index that affected the policy table
			index, err := s.Index(state.TableRootKeyMeta)
			if err != nil {
				return err
			}

			// Ensure we never set the index to zero, otherwise a blocking query
			// cannot be used.  We floor the index at one, since realistically
			// the first write must have a higher index.
			if index == 0 {
				index = 1
			}
			reply.Index = index
			return nil
		},
	}
	return k.srv.blockingRPC(&opts)
}

func (k *Keyring) Delete(args *structs.KeyringDeleteRootKeyRequest, reply *structs.KeyringDeleteRootKeyResponse) error {

	authErr := k.srv.Authenticate(k.ctx, args)
	if done, err := k.srv.forward("Keyring.Delete", args, args, reply); done {
		return err
	}
	k.srv.MeasureRPCRate("keyring", structs.RateMetricWrite, args)
	if authErr != nil {
		return structs.ErrPermissionDenied
	}

	defer metrics.MeasureSince([]string{"nomad", "keyring", "delete"}, time.Now())

	if aclObj, err := k.srv.ResolveACL(args); err != nil {
		return err
	} else if !aclObj.IsManagement() {
		return structs.ErrPermissionDenied
	}

	if args.KeyID == "" {
		return fmt.Errorf("root key ID is required")
	}

	// lookup any existing key and validate the delete
	var index uint64
	snap, err := k.srv.fsm.State().Snapshot()
	if err != nil {
		return err
	}
	ws := memdb.NewWatchSet()
	wrappedKey, err := snap.WrappedRootKeysByID(ws, args.KeyID)
	if err != nil {
		return err
	}
	if wrappedKey == nil {
		// COMPAT(1.12.0): remove this query and bail out early with "no such
		// root key" error
		keyMeta, err := snap.RootKeyMetaByID(ws, args.KeyID)
		if err != nil {
			return err
		}
		if keyMeta == nil {
			return fmt.Errorf("no such root key %q", args.KeyID)
		}
		if keyMeta.IsActive() {
			return fmt.Errorf("active root key cannot be deleted - call rotate first")
		}

		_, index, err = k.srv.raftApply(structs.RootKeyMetaDeleteRequestType, args)
		if err != nil {
			return err
		}

	} else {
		if wrappedKey.Meta.IsActive() {
			return fmt.Errorf("active root key cannot be deleted - call rotate first")
		}
		_, index, err = k.srv.raftApply(structs.WrappedRootKeysDeleteRequestType, args)
		if err != nil {
			return err
		}
	}

	// remove the key from the keyring too
	k.encrypter.RemoveKey(args.KeyID)

	reply.Index = index
	return nil
}

// ListPublic signing keys used for workload identities. This RPC is used to
// back a JWKS endpoint.
//
// Unauthenticated because public keys are not sensitive.
func (k *Keyring) ListPublic(args *structs.GenericRequest, reply *structs.KeyringListPublicResponse) error {

	// JWKS is a public endpoint: intentionally ignore auth errors and only
	// authenticate to measure rate metrics.
	k.srv.Authenticate(k.ctx, args)
	if done, err := k.srv.forward("Keyring.ListPublic", args, args, reply); done {
		return err
	}
	k.srv.MeasureRPCRate("keyring", structs.RateMetricList, args)

	defer metrics.MeasureSince([]string{"nomad", "keyring", "list_public"}, time.Now())

	// Expose root_key_rotation_threshold so consumers can determine reasonable
	// cache settings.
	reply.RotationThreshold = k.srv.config.RootKeyRotationThreshold

	// Setup the blocking query
	opts := blockingOptions{
		queryOpts: &args.QueryOptions,
		queryMeta: &reply.QueryMeta,
		run: func(ws memdb.WatchSet, store *state.StateStore) error {

			keys, index, err := k.listAllKeyMetadata(ws, store)
			if err != nil {
				return err
			}

			pubKeys := []*structs.KeyringPublicKey{}
			for _, keyMeta := range keys {
				if keyMeta.State == structs.RootKeyStateDeprecated {
					// Only include valid keys
					continue
				}

				pubKey, err := k.encrypter.GetPublicKey(keyMeta.KeyID)
				if err != nil {
					return err
				}

				pubKeys = append(pubKeys, pubKey)
			}

			reply.PublicKeys = pubKeys

			// COMPAT(1.12.0): once the RootKeyMeta query is removed we can call
			// replySetIndex here
			reply.Index = index
			k.srv.setQueryMeta(&reply.QueryMeta)
			return nil
		},
	}
	return k.srv.blockingRPC(&opts)
}

// listAllKeyMetadata queries both the WrappedRootKeys and legacy RootKeyMeta
// tables to return a list of all the active key metadata and the latest index
// between the two tables.
//
// COMPAT(1.12.0): remove this method and update callers to just iterate
// WrappedRootKeys and call replySetIndex
func (k *Keyring) listAllKeyMetadata(ws memdb.WatchSet, store *state.StateStore) ([]*structs.RootKeyMeta, uint64, error) {

	snap, err := store.Snapshot()
	if err != nil {
		return nil, 0, err
	}

	keys := []*structs.RootKeyMeta{}

	iter, err := snap.WrappedRootKeys(ws)
	if err != nil {
		return nil, 0, err
	}
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		wrappedKey := raw.(*structs.WrappedRootKeys)
		keys = append(keys, wrappedKey.Meta)
	}
	index, err := snap.Index(state.TableWrappedRootKeys)
	if err != nil {
		return nil, 0, err
	}

	iter, err = snap.RootKeyMetas(ws)
	if err != nil {
		return nil, 0, err
	}
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		keyMeta := raw.(*structs.RootKeyMeta)
		keys = append(keys, keyMeta)
	}
	legacyIndex, err := snap.Index(state.TableWrappedRootKeys)
	if err != nil {
		return nil, 0, err
	}
	if legacyIndex > index {
		index = legacyIndex
	}

	return keys, index, nil
}

// GetConfig for workload identities. This RPC is used to back an OIDC
// Discovery endpoint.
//
// Unauthenticated because OIDC Discovery endpoints must be publically
// available.
func (k *Keyring) GetConfig(args *structs.GenericRequest, reply *structs.KeyringGetConfigResponse) error {

	// JWKS is a public endpoint: intentionally ignore auth errors and only
	// authenticate to measure rate metrics.
	k.srv.Authenticate(k.ctx, args)
	if done, err := k.srv.forward("Keyring.GetConfig", args, args, reply); done {
		return err
	}
	k.srv.MeasureRPCRate("keyring", structs.RateMetricList, args)

	defer metrics.MeasureSince([]string{"nomad", "keyring", "get_config"}, time.Now())

	reply.OIDCDiscovery = k.srv.oidcDisco
	return nil
}

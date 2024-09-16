// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: BUSL-1.1

package nomad

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/hashicorp/go-hclog"
	kms "github.com/hashicorp/go-kms-wrapping/v2"
	"github.com/hashicorp/go-kms-wrapping/v2/aead"
	"github.com/hashicorp/go-kms-wrapping/wrappers/awskms/v2"
	"github.com/hashicorp/go-kms-wrapping/wrappers/azurekeyvault/v2"
	"github.com/hashicorp/go-kms-wrapping/wrappers/gcpckms/v2"
	"github.com/hashicorp/go-kms-wrapping/wrappers/transit/v2"
	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/helper/crypto"
	"github.com/hashicorp/nomad/helper/joseutil"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/hashicorp/nomad/nomad/structs/config"
	"github.com/hashicorp/raft"
	"golang.org/x/exp/maps"
	"golang.org/x/time/rate"
)

const nomadKeystoreExtension = ".nks.json"

type claimSigner interface {
	SignClaims(*structs.IdentityClaims) (string, string, error)
}

var _ claimSigner = &Encrypter{}

// Encrypter is the keyring for encrypting variables and signing workload
// identities.
type Encrypter struct {
	srv             *Server
	log             hclog.Logger
	providerConfigs map[string]*structs.KEKProviderConfig
	keystorePath    string

	// issuer is the OIDC Issuer to use for workload identities if configured
	issuer string

	keyring      map[string]*keyset
	decryptTasks map[string]context.CancelFunc
	lock         sync.RWMutex
}

// keyset contains the key material for variable encryption and workload
// identity signing. As keysets are rotated they are identified by the RootKey
// KeyID although the public key IDs are published with a type prefix to
// disambiguate which signing algorithm to use.
type keyset struct {
	rootKey           *structs.RootKey
	cipher            cipher.AEAD
	eddsaPrivateKey   ed25519.PrivateKey
	rsaPrivateKey     *rsa.PrivateKey
	rsaPKCS1PublicKey []byte // PKCS #1 DER encoded public key for JWKS
}

// NewEncrypter loads or creates a new local keystore and returns an
// encryption keyring with the keys it finds.
func NewEncrypter(srv *Server, keystorePath string) (*Encrypter, error) {

	encrypter := &Encrypter{
		srv:             srv,
		log:             srv.logger.Named("keyring"),
		keystorePath:    keystorePath,
		keyring:         make(map[string]*keyset),
		issuer:          srv.GetConfig().OIDCIssuer,
		providerConfigs: map[string]*structs.KEKProviderConfig{},
		decryptTasks:    map[string]context.CancelFunc{},
	}

	providerConfigs, err := getProviderConfigs(srv)
	if err != nil {
		return nil, err
	}
	encrypter.providerConfigs = providerConfigs

	err = encrypter.loadKeystore()
	if err != nil {
		return nil, err
	}
	return encrypter, nil
}

// fallbackVaultConfig allows the transit provider to fallback to using the
// default Vault cluster's configuration block, instead of repeating those
// fields
func fallbackVaultConfig(provider *structs.KEKProviderConfig, vaultcfg *config.VaultConfig) {

	setFallback := func(key, fallback, env string) {
		if provider.Config == nil {
			provider.Config = map[string]string{}
		}
		if _, ok := provider.Config[key]; !ok {
			if fallback != "" {
				provider.Config[key] = fallback
			} else {
				provider.Config[key] = os.Getenv(env)
			}
		}
	}

	setFallback("address", vaultcfg.Addr, "VAULT_ADDR")
	setFallback("token", vaultcfg.Token, "VAULT_TOKEN")
	setFallback("tls_ca_cert", vaultcfg.TLSCaPath, "VAULT_CACERT")
	setFallback("tls_client_cert", vaultcfg.TLSCertFile, "VAULT_CLIENT_CERT")
	setFallback("tls_client_key", vaultcfg.TLSKeyFile, "VAULT_CLIENT_KEY")
	setFallback("tls_server_name", vaultcfg.TLSServerName, "VAULT_TLS_SERVER_NAME")

	skipVerify := ""
	if vaultcfg.TLSSkipVerify != nil {
		skipVerify = fmt.Sprintf("%v", *vaultcfg.TLSSkipVerify)
	}
	setFallback("tls_skip_verify", skipVerify, "VAULT_SKIP_VERIFY")
}

func (e *Encrypter) loadKeystore() error {

	if err := os.MkdirAll(e.keystorePath, 0o700); err != nil {
		return err
	}

	keyErrors := map[string]error{}

	return filepath.Walk(e.keystorePath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("could not read path %s from keystore: %v", path, err)
		}

		// skip over subdirectories and non-key files; they shouldn't
		// be here but there's no reason to fail startup for it if the
		// administrator has left something there
		if path != e.keystorePath && info.IsDir() {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, nomadKeystoreExtension) {
			return nil
		}
		idWithIndex := strings.TrimSuffix(filepath.Base(path), nomadKeystoreExtension)
		id, _, _ := strings.Cut(idWithIndex, ".")
		if !helper.IsUUID(id) {
			return nil
		}

		//		e.log.Info("LOCK (read) - filepath.Walk locking") // TODO: remove me
		e.lock.RLock()
		//		e.log.Info("LOCK (read) - filepath.Walk locked") // TODO: remove me
		_, ok := e.keyring[id]
		e.lock.RUnlock()
		//		e.log.Info("LOCK (read) - filepath.Walk unlocked") // TODO: remove me
		if ok {
			return nil // already loaded this key from another file
		}

		key, err := e.loadKeyFromStore(path)
		if err != nil {
			keyErrors[id] = err
			return fmt.Errorf("could not load key file %s from keystore: %w", path, err)
		}
		if key.Meta.KeyID != id {
			return fmt.Errorf("root key ID %s must match key file %s", key.Meta.KeyID, path)
		}

		err = e.addCipher(key)
		if err != nil {
			return fmt.Errorf("could not add key file %s to keystore: %w", path, err)
		}

		// we loaded this key from at least one KEK configuration, so clear any
		// error from a previous file that we couldn't read from
		delete(keyErrors, id)
		return nil
	})
}

func (e *Encrypter) IsReady(ctx context.Context) error {
	err := helper.WithBackoffFunc(ctx, time.Millisecond*100, time.Second, func() error {
		//		e.log.Info("LOCK (read) - IsReady locking") // TODO: remove me
		e.lock.RLock()
		//		e.log.Info("LOCK (read) - IsReady locked") // TODO: remove me
		//		defer e.lock.RUnlock()
		defer func() {
			defer e.lock.RUnlock()
			//			e.log.Info("LOCK (read) - IsReady unlocked") // TODO: remove me
		}()
		if len(e.decryptTasks) != 0 {
			return fmt.Errorf("keyring is not ready - waiting for keys %s",
				maps.Keys(e.decryptTasks))
		}
		return nil
	})
	if err != nil {
		return err
	}
	e.log.Warn("Encrypter.IsReady!")
	return nil
}

// Encrypt encrypts the clear data with the cipher for the current
// root key, and returns the cipher text (including the nonce), and
// the key ID used to encrypt it
func (e *Encrypter) Encrypt(cleartext []byte) ([]byte, string, error) {

	keyset, err := e.activeKeySet()
	if err != nil {
		return nil, "", err
	}

	nonce, err := crypto.Bytes(keyset.cipher.NonceSize())
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate key wrapper nonce: %v", err)
	}

	keyID := keyset.rootKey.Meta.KeyID
	additional := []byte(keyID) // include the keyID in the signature inputs

	// we use the nonce as the dst buffer so that the ciphertext is
	// appended to that buffer and we always keep the nonce and
	// ciphertext together, and so that we're not tempted to reuse
	// the cleartext buffer which the caller still owns
	ciphertext := keyset.cipher.Seal(nonce, nonce, cleartext, additional)
	return ciphertext, keyID, nil
}

// Decrypt takes an encrypted buffer and then root key ID. It extracts
// the nonce, decrypts the content, and returns the cleartext data.
func (e *Encrypter) Decrypt(ciphertext []byte, keyID string) ([]byte, error) {

	ctx, cancel := context.WithTimeout(e.srv.shutdownCtx, time.Second)
	defer cancel()
	ks, err := e.waitForKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	nonceSize := ks.cipher.NonceSize()
	nonce := ciphertext[:nonceSize] // nonce was stored alongside ciphertext
	additional := []byte(keyID)     // keyID was included in the signature inputs

	return ks.cipher.Open(nil, nonce, ciphertext[nonceSize:], additional)
}

// keyIDHeader is the JWT header for the Nomad Key ID used to sign the
// claim. This name matches the common industry practice for this
// header name.
const keyIDHeader = "kid"

// SignClaims signs the identity claim for the task and returns an encoded JWT
// (including both the claim and its signature) and the key ID of the key used
// to sign it, or an error.
//
// SignClaims adds the Issuer claim prior to signing.
func (e *Encrypter) SignClaims(claims *structs.IdentityClaims) (string, string, error) {

	if claims == nil {
		return "", "", errors.New("cannot sign empty claims")
	}

	//  DEBUG remove this log line
	e.log.Info("SignClaims querying activeKeySet")
	ks, err := e.activeKeySet()
	if err != nil {
		return "", "", err
	}

	// Add Issuer claim from server configuration
	if e.issuer != "" {
		claims.Issuer = e.issuer
	}

	opts := (&jose.SignerOptions{}).WithHeader("kid", ks.rootKey.Meta.KeyID).WithType("JWT")

	var sig jose.Signer
	if ks.rsaPrivateKey != nil {
		// If an RSA key has been created prefer it as it is more widely compatible
		sig, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: ks.rsaPrivateKey}, opts)
		if err != nil {
			return "", "", err
		}
	} else {
		// No RSA key has been created, fallback to ed25519 which always exists
		sig, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.EdDSA, Key: ks.eddsaPrivateKey}, opts)
		if err != nil {
			return "", "", err
		}
	}

	raw, err := jwt.Signed(sig).Claims(claims).CompactSerialize()
	if err != nil {
		return "", "", err
	}

	return raw, ks.rootKey.Meta.KeyID, nil
}

// VerifyClaim accepts a previously-signed encoded claim and validates
// it before returning the claim
func (e *Encrypter) VerifyClaim(tokenString string) (*structs.IdentityClaims, error) {

	token, err := jwt.ParseSigned(tokenString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signed token: %w", err)
	}

	// Find the Key ID
	keyID, err := joseutil.KeyID(token)
	if err != nil {
		return nil, err
	}

	// Find the key material
	pubKey, err := e.waitForPublicKey(keyID)
	if err != nil {
		return nil, err
	}

	typedPubKey, err := pubKey.GetPublicKey()
	if err != nil {
		return nil, err
	}

	// Validate the claims.
	claims := &structs.IdentityClaims{}
	if err := token.Claims(typedPubKey, claims); err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	//COMPAT Until we can guarantee there are no pre-1.7 JWTs in use we can only
	//       validate the signature and have no further expectations of the
	//       claims.
	expect := jwt.Expected{}
	if err := claims.Validate(expect); err != nil {
		return nil, fmt.Errorf("invalid claims: %w", err)
	}

	return claims, nil
}

// AddUnwrappedKey stores the key in the keystore and creates a new cipher for
// it. This is called in the RPC handlers on the leader and from the legacy
// KeyringReplicator.
func (e *Encrypter) AddUnwrappedKey(rootKey *structs.RootKey, isUpgraded bool) (*structs.WrappedRootKeys, error) {

	// note: we don't lock the keyring here but inside addCipher
	// instead, so that we're not holding the lock while performing
	// local disk writes
	if err := e.addCipher(rootKey); err != nil {
		return nil, err
	}
	return e.saveKeyToStore(rootKey, isUpgraded)
}

// AddWrappedKey creates decryption tasks for keys we've previously stored in
// Raft. It's only called as a goroutine by the FSM Apply for WrappedRootKeys,
// but it returns an error for ease of testing.
func (e *Encrypter) AddWrappedKey(ctx context.Context, wrappedKeys *structs.WrappedRootKeys) error {

	logger := e.log.With("key_id", wrappedKeys.Meta.KeyID)

	// DEBUG: remove this log
	logger.Warn("calling AddWrappedKey")

	meta := wrappedKeys.Meta

	//	e.log.Info("LOCK (r/w) - AddWrappedKey locking") // TODO: remove me
	e.lock.Lock()
	//	e.log.Info("LOCK (r/w) - AddWrappedKey locked") // TODO: remove me
	// DEBUG: remove this log
	//	logger.Warn("AddWrappedKey: checking keysetByIDLocked")

	_, err := e.keysetByIDLocked(meta.KeyID)
	if err == nil {
		// DEBUG: remove this log
		logger.Warn("AddWrappedKey: keysetByIDLocked found key in keyring")

		// key material for each key ID is immutable so nothing to do, but we
		// can cancel and remove any running decrypt tasks
		if cancel, ok := e.decryptTasks[meta.KeyID]; ok {
			cancel()
			delete(e.decryptTasks, meta.KeyID)
		}
		e.lock.Unlock()
		//		e.log.Info("LOCK (r/w) - AddWrappedKey unlocked") // TODO: remove me
		logger.Warn("AddWrappedKey done! (no-op)")
		return nil
	}

	if cancel, ok := e.decryptTasks[meta.KeyID]; ok {
		// stop any previous tasks for this same key ID under the assumption
		// they're broken or being superceded, but don't remove the CancelFunc
		// from the map yet so that other callers don't think we can continue
		cancel()
	}

	e.lock.Unlock()
	//	e.log.Info("LOCK (r/w) - AddWrappedKey unlocked") // TODO: remove me
	// DEBUG: remove this log
	logger.Warn("AddWrappedKey: no existing key found by GetKey, moving to decrypt")

	completeCtx, cancel := context.WithCancel(ctx)

	//spew.Dump(wrappedKeys)

	for _, wrappedKey := range wrappedKeys.WrappedKeys {
		providerID := wrappedKey.ProviderID
		if providerID == "" {
			providerID = string(structs.KEKProviderAEAD)
		}

		// DEBUG: remove this log
		logger.Warn("AddWrappedKey: unwrapping for provider", "provider", providerID)

		provider, ok := e.providerConfigs[providerID]
		if !ok {
			logger.Error("no such KMS provider configured - root key cannot be decrypted unless another provider is available", "provider_id", providerID)
			return fmt.Errorf("no such provider %q configured", providerID)
		}

		// DEBUG: remove this log
		logger.Warn("AddWrappedKey: found provider", "provider", providerID)

		if provider.UseKeystore() {
			// this key is store_keys_outside_raft=true, so we need to get the
			// KEK via RPC
			go e.replicateFromPeers(ctx, cancel, meta, wrappedKey)
			continue
		}

		wrapper, err := e.newKMSWrapper(provider, meta.KeyID, wrappedKey.KeyEncryptionKey)
		if err != nil {
			// the errors that bubble up from this library can be a bit opaque, so
			// make sure we wrap them with as much context as possible
			logger.Error("unable to create KMS wrapper - root key cannot be decrypted unless another provider is available", "provider_id", providerID, "error", err)

			return fmt.Errorf("unable to create key wrapper for provider %q: %w", providerID, err)
		}

		// DEBUG: remove this
		logger.Warn("AddWrappedKey: started decrypt task", "provider", provider.ID())

		// fan-out decryption tasks for HA in Nomad Enterprise. we can use the
		// key whenever any one provider returns a successful decryption
		go e.decryptWrappedKeyTask(completeCtx, cancel, wrapper, provider, meta, wrappedKey)
	}

	//	e.log.Info("LOCK (r/w) - AddWrappedKey locking (2)") // TODO: remove me
	e.lock.Lock()
	//	e.log.Info("LOCK (r/w) - AddWrappedKey locked (2)") // TODO: remove me
	defer func() {
		e.lock.Unlock()
		//		e.log.Info("LOCK (r/w) - AddWrappedKey locked (2)") // TODO: remove me
	}()

	e.decryptTasks[meta.KeyID] = cancel

	// DEBUG: remove this log
	logger.Warn("call to AddWrappedKey done!")

	return nil
}

func (e *Encrypter) replicateFromPeers(ctx context.Context, cancel context.CancelFunc, meta *structs.RootKeyMeta, wrappedKey *structs.WrappedRootKey) {
	e.log.Warn("start fetch from peers task", "key_id", meta.KeyID)
	helper.WithBackoffFunc(ctx, time.Millisecond*100, time.Second, func() error {
		if err := e.srv.replicateKey(ctx, e.log, meta); err != nil {
			// we want this to be noisy in the logs, so log each time we fail to
			// replicate
			e.log.Error(err.Error(), "key_id", meta.KeyID)
			return err
		}
		return nil
	})

	// notify all other tasks that we've completed the decryption
	//	e.log.Info("LOCK (r/w) - replicateFromPeers locking") // TODO: remove me
	e.lock.Lock()
	//	e.log.Info("LOCK (r/w) - replicateFromPeers locked") // TODO: remove me
	cancel()
	defer func() {
		e.lock.Unlock()
		//	e.log.Info("LOCK (r/w) - replicateFromPeers unlocked") // TODO: remove me
	}()
	delete(e.decryptTasks, meta.KeyID)
	// DEBUG: remove this
	e.log.Warn("fetch from peers task: done", "key_id", meta.KeyID)
}

func (e *Encrypter) decryptWrappedKeyTask(ctx context.Context, cancel context.CancelFunc, wrapper kms.Wrapper, provider *structs.KEKProviderConfig, meta *structs.RootKeyMeta, wrappedKey *structs.WrappedRootKey) {

	var key []byte
	var rsaKey []byte
	var err error

	minBackoff := time.Second
	maxBackoff := time.Second * 30

	helper.WithBackoffFunc(ctx, minBackoff, maxBackoff, func() error {
		wrappedDEK := wrappedKey.WrappedDataEncryptionKey
		key, err = wrapper.Decrypt(e.srv.shutdownCtx, wrappedDEK)
		if err != nil {
			err := fmt.Errorf("%w (root key): %w", ErrDecryptFailed, err)
			e.log.Error(err.Error(), "key_id", meta.KeyID)
			return err
		}
		return nil
	})

	// DEBUG: remove this
	e.log.Warn("decrypt task: DEK decrypted", "key_id", meta.KeyID)

	helper.WithBackoffFunc(ctx, minBackoff, maxBackoff, func() error {
		// Decrypt RSAKey for Workload Identity JWT signing if one exists. Prior to
		// 1.7 an ed25519 key derived from the root key was used instead of an RSA
		// key.
		if wrappedKey.WrappedRSAKey != nil {
			rsaKey, err = wrapper.Decrypt(e.srv.shutdownCtx, wrappedKey.WrappedRSAKey)
			if err != nil {
				err := fmt.Errorf("%w (rsa key): %w", ErrDecryptFailed, err)
				e.log.Error(err.Error(), "key_id", meta.KeyID)
			}
		}
		return nil
	})

	// DEBUG: remove this
	e.log.Warn("decrypt task: RSAKey decrypted", "key_id", meta.KeyID)

	rootKey := &structs.RootKey{
		Meta:   meta,
		Key:    key,
		RSAKey: rsaKey,
	}

	helper.WithBackoffFunc(ctx, minBackoff, maxBackoff, func() error {
		err = e.addCipher(rootKey)
		if err != nil {
			err := fmt.Errorf("could not add cipher: %w", err)
			e.log.Error(err.Error(), "key_id", meta.KeyID)
			return err
		}
		return nil
	})

	// DEBUG: remove this
	e.log.Warn("decrypt task: ciper added", "key_id", meta.KeyID)

	// notify all other tasks that we've completed the decryption
	//	e.log.Info("LOCK (r/w) - decryptWrappedKeyTask locking") // TODO: remove me
	e.lock.Lock()
	//	e.log.Info("LOCK (r/w) - decryptWrappedKeyTask locked") // TODO: remove me
	cancel()
	defer func() {
		e.lock.Unlock()
		//	e.log.Info("LOCK (r/w) - decryptWrappedKeyTask unlocked") // TODO: remove me
	}()
	delete(e.decryptTasks, meta.KeyID)
	// DEBUG: remove this
	e.log.Warn("decrypt task: done", "key_id", meta.KeyID)
}

// addCipher stores the key in the keyring and creates a new cipher for it.
func (e *Encrypter) addCipher(rootKey *structs.RootKey) error {

	if rootKey == nil || rootKey.Meta == nil {
		return fmt.Errorf("missing metadata")
	}
	var aead cipher.AEAD

	switch rootKey.Meta.Algorithm {
	case structs.EncryptionAlgorithmAES256GCM:
		block, err := aes.NewCipher(rootKey.Key)
		if err != nil {
			return fmt.Errorf("could not create cipher: %v", err)
		}
		aead, err = cipher.NewGCM(block)
		if err != nil {
			return fmt.Errorf("could not create cipher: %v", err)
		}
	default:
		return fmt.Errorf("invalid algorithm %s", rootKey.Meta.Algorithm)
	}

	ed25519Key := ed25519.NewKeyFromSeed(rootKey.Key)

	ks := keyset{
		rootKey:         rootKey,
		cipher:          aead,
		eddsaPrivateKey: ed25519Key,
	}

	// Unmarshal RSAKey for Workload Identity JWT signing if one exists. Prior to
	// 1.7 only the ed25519 key was used.
	if len(rootKey.RSAKey) > 0 {
		rsaKey, err := x509.ParsePKCS1PrivateKey(rootKey.RSAKey)
		if err != nil {
			return fmt.Errorf("error parsing rsa key: %w", err)
		}

		ks.rsaPrivateKey = rsaKey
		ks.rsaPKCS1PublicKey = x509.MarshalPKCS1PublicKey(&rsaKey.PublicKey)
	}

	//	e.log.Info("LOCK (r/w) - addCipher locking") // TODO: remove me
	e.lock.Lock()
	//	e.log.Info("LOCK (r/w) - addCipher locked") // TODO: remove me
	defer func() {
		e.lock.Unlock()
		//	e.log.Info("LOCK (r/w) - addCipher unlocked") // TODO: remove me
	}()
	e.keyring[rootKey.Meta.KeyID] = &ks
	return nil
}

// waitForKey retrieves the key material by ID from the keyring, retrying with
// geometric backoff until the context expires.
func (e *Encrypter) waitForKey(ctx context.Context, keyID string) (*keyset, error) {
	var ks *keyset
	var err error

	helper.WithBackoffFunc(ctx, 50*time.Millisecond, 100*time.Millisecond,
		func() error {
			e.lock.RLock()
			defer e.lock.RUnlock()
			var err error
			ks, err = e.keysetByIDLocked(keyID)
			if err != nil {
				return err
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	if ks == nil {
		return nil, fmt.Errorf("no such key")
	}
	return ks, nil
}

// GetKey retrieves the key material by ID from the keyring.
func (e *Encrypter) GetKey(keyID string) (*structs.RootKey, error) {
	e.lock.Lock()
	defer e.lock.Unlock()

	ks, err := e.keysetByIDLocked(keyID)
	if err != nil {
		return nil, err
	}
	if ks == nil {
		return nil, fmt.Errorf("no such key")
	}

	return ks.rootKey, nil
}

// activeKeySetLocked returns the keyset that belongs to the key marked as
// active in the state store (so that it's consistent with raft).
//
// If a key is rotated immediately following a leader election, plans that are
// in-flight may get signed before the new leader has decrypted the key. Allow
// for a short timeout-and-retry to avoid rejecting plans
func (e *Encrypter) activeKeySet() (*keyset, error) {
	store := e.srv.fsm.State()
	keyMeta, err := store.GetActiveRootKeyMeta(nil)
	if err != nil {
		return nil, err
	}
	if keyMeta == nil {
		return nil, fmt.Errorf("keyring has not been initialized yet")
	}

	ctx, cancel := context.WithTimeout(e.srv.shutdownCtx, time.Second)
	defer cancel()
	return e.waitForKey(ctx, keyMeta.KeyID)
}

// keysetByIDLocked returns the keyset for the specified keyID. The
// caller must read-lock the keyring
func (e *Encrypter) keysetByIDLocked(keyID string) (*keyset, error) {
	keyset, ok := e.keyring[keyID]
	if !ok {
		return nil, fmt.Errorf("no such key %q in keyring", keyID)
	}
	return keyset, nil
}

// RemoveKey removes a key by ID from the keyring
func (e *Encrypter) RemoveKey(keyID string) error {
	e.lock.Lock()
	defer e.lock.Unlock()
	delete(e.keyring, keyID)
	return nil
}

func (e *Encrypter) encryptDEK(rootKey *structs.RootKey, provider *structs.KEKProviderConfig) (*structs.WrappedRootKey, error) {
	if provider == nil {
		panic("can't encrypt DEK without a provider")
	}
	var kek []byte
	var err error
	if provider.Provider == string(structs.KEKProviderAEAD) || provider.Provider == "" {
		kek, err = crypto.Bytes(32)
		if err != nil {
			return nil, fmt.Errorf("failed to generate key wrapper key: %w", err)
		}
	}
	wrapper, err := e.newKMSWrapper(provider, rootKey.Meta.KeyID, kek)
	if err != nil {
		return nil, fmt.Errorf("unable to create key wrapper: %w", err)
	}

	rootBlob, err := wrapper.Encrypt(e.srv.shutdownCtx, rootKey.Key)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt root key: %w", err)
	}

	kekWrapper := &structs.WrappedRootKey{
		Provider:                 provider.Provider,
		ProviderID:               provider.ID(),
		WrappedDataEncryptionKey: rootBlob,
		WrappedRSAKey:            &kms.BlobInfo{},
		KeyEncryptionKey:         kek,
	}

	// Only keysets created after 1.7.0 will contain an RSA key.
	if len(rootKey.RSAKey) > 0 {
		rsaBlob, err := wrapper.Encrypt(e.srv.shutdownCtx, rootKey.RSAKey)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt rsa key: %w", err)
		}
		kekWrapper.WrappedRSAKey = rsaBlob
	}

	return kekWrapper, nil
}

// saveKeyToStore serializes a root key to the on-disk keystore.
//
// TODO: this name doesn't fit well now that we have KMS
func (e *Encrypter) saveKeyToStore(rootKey *structs.RootKey, isUpgraded bool) (*structs.WrappedRootKeys, error) {

	wrappedKeys := &structs.WrappedRootKeys{
		Meta:        rootKey.Meta,
		WrappedKeys: []*structs.WrappedRootKey{},
	}

	for _, provider := range e.providerConfigs {
		if !provider.Active {
			continue
		}
		wrappedKey, err := e.encryptDEK(rootKey, provider)
		if err != nil {
			return nil, err
		}

		//spew.Dump(provider)

		switch {
		case isUpgraded && provider.UseKeystore():
			e.log.Warn("writing wrapped key to Raft w/o KEK and on-disk keystore w/ KEK")
			kek := wrappedKey.KeyEncryptionKey
			wrappedKey.KeyEncryptionKey = nil
			e.writeKeyToDisk(rootKey.Meta, provider, wrappedKey, kek)

		case isUpgraded && provider.UseCleartextKEKinRaft():
			e.log.Warn("writing wrapped key to Raft w/ KEK")
			// nothing to do but don't want to hit next case

		case isUpgraded:
			e.log.Warn("writing wrapped key to Raft w/o KEK using KMS")
			wrappedKey.KeyEncryptionKey = nil

		case provider.Provider == string(structs.KEKProviderAEAD):
			e.log.Warn("writing wrapped key to Raft w/o KEK and and on-disk keystore w/ KEK")
			kek := wrappedKey.KeyEncryptionKey
			wrappedKey.KeyEncryptionKey = nil
			e.writeKeyToDisk(rootKey.Meta, provider, wrappedKey, kek)

		default:
			e.log.Warn("writing wrapped key to Raft w/o KEK and on-disk keystore w/ KMS")
			wrappedKey.KeyEncryptionKey = nil
			e.writeKeyToDisk(rootKey.Meta, provider, wrappedKey, nil)
		}

		wrappedKeys.WrappedKeys = append(wrappedKeys.WrappedKeys, wrappedKey)

	}
	e.log.Warn("saveToKeystore done")
	return wrappedKeys, nil
}

func (e *Encrypter) writeKeyToDisk(
	meta *structs.RootKeyMeta, provider *structs.KEKProviderConfig,
	wrappedKey *structs.WrappedRootKey, kek []byte) error {

	// the on-disk keystore flattens the keys wrapped for the individual
	// KMS providers out to their own files
	diskWrapper := &structs.KeyEncryptionKeyWrapper{
		Meta:                     meta,
		Provider:                 provider.Name,
		ProviderID:               provider.ID(),
		WrappedDataEncryptionKey: wrappedKey.WrappedDataEncryptionKey,
		WrappedRSAKey:            wrappedKey.WrappedRSAKey,
		KeyEncryptionKey:         kek,
	}

	buf, err := json.Marshal(diskWrapper)
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.%s%s",
		meta.KeyID, provider.ID(), nomadKeystoreExtension)
	path := filepath.Join(e.keystorePath, filename)
	err = os.WriteFile(path, buf, 0o600)
	if err != nil {
		return err
	}
	return nil
}

// loadKeyFromStore deserializes a root key from disk.
func (e *Encrypter) loadKeyFromStore(path string) (*structs.RootKey, error) {

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	kekWrapper := &structs.KeyEncryptionKeyWrapper{}
	if err := json.Unmarshal(raw, kekWrapper); err != nil {
		return nil, err
	}

	meta := kekWrapper.Meta
	if err = meta.Validate(); err != nil {
		return nil, err
	}

	//spew.Dump(kekWrapper)

	if kekWrapper.ProviderID == "" {
		kekWrapper.ProviderID = string(structs.KEKProviderAEAD)
	}
	provider, ok := e.providerConfigs[kekWrapper.ProviderID]
	if !ok {
		return nil, fmt.Errorf("no such provider %q configured", kekWrapper.ProviderID)
	}

	// the errors that bubble up from this library can be a bit opaque, so make
	// sure we wrap them with as much context as possible
	wrapper, err := e.newKMSWrapper(provider, meta.KeyID, kekWrapper.KeyEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("unable to create key wrapper: %w", err)
	}
	wrappedDEK := kekWrapper.WrappedDataEncryptionKey
	if wrappedDEK == nil {
		// older KEK wrapper versions with AEAD-only have the key material in a
		// different field
		wrappedDEK = &kms.BlobInfo{Ciphertext: kekWrapper.EncryptedDataEncryptionKey}
	}
	key, err := wrapper.Decrypt(e.srv.shutdownCtx, wrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("%w (root key): %w", ErrDecryptFailed, err)
	}

	// Decrypt RSAKey for Workload Identity JWT signing if one exists. Prior to
	// 1.7 an ed25519 key derived from the root key was used instead of an RSA
	// key.
	var rsaKey []byte
	if kekWrapper.WrappedRSAKey != nil {
		rsaKey, err = wrapper.Decrypt(e.srv.shutdownCtx, kekWrapper.WrappedRSAKey)
		if err != nil {
			return nil, fmt.Errorf("%w (rsa key): %w", ErrDecryptFailed, err)
		}
	} else if len(kekWrapper.EncryptedRSAKey) > 0 {
		// older KEK wrapper versions with AEAD-only have the key material in a
		// different field
		rsaKey, err = wrapper.Decrypt(e.srv.shutdownCtx, &kms.BlobInfo{
			Ciphertext: kekWrapper.EncryptedRSAKey})
		if err != nil {
			return nil, fmt.Errorf("%w (rsa key): %w", ErrDecryptFailed, err)
		}
	}

	return &structs.RootKey{
		Meta:   meta,
		Key:    key,
		RSAKey: rsaKey,
	}, nil
}

var ErrDecryptFailed = errors.New("unable to decrypt wrapped key")

// waitForPublicKey returns the public signing key for the requested key id or
// an error if the key could not be found. It blocks up to 1s for key material
// to be decrypted so that Workload Identities signed by a brand-new key can be
// verified for stale RPCs made to followers that might not have yet decrypted
// the key received via Raft
func (e *Encrypter) waitForPublicKey(keyID string) (*structs.KeyringPublicKey, error) {
	ctx, cancel := context.WithTimeout(e.srv.shutdownCtx, 1*time.Second)
	defer cancel()
	ks, err := e.waitForKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	pubKey := &structs.KeyringPublicKey{
		KeyID:      keyID,
		Use:        structs.PubKeyUseSig,
		CreateTime: ks.rootKey.Meta.CreateTime,
	}

	if ks.rsaPrivateKey != nil {
		pubKey.PublicKey = ks.rsaPKCS1PublicKey
		pubKey.Algorithm = structs.PubKeyAlgRS256
	} else {
		pubKey.PublicKey = ks.eddsaPrivateKey.Public().(ed25519.PublicKey)
		pubKey.Algorithm = structs.PubKeyAlgEdDSA
	}

	return pubKey, nil
}

// GetPublicKey returns the public signing key for the requested key id or an
// error if the key could not be found.
func (e *Encrypter) GetPublicKey(keyID string) (*structs.KeyringPublicKey, error) {
	e.lock.RLock()
	defer e.lock.RUnlock()

	ks, err := e.keysetByIDLocked(keyID)
	if err != nil {
		return nil, err
	}

	pubKey := &structs.KeyringPublicKey{
		KeyID:      keyID,
		Use:        structs.PubKeyUseSig,
		CreateTime: ks.rootKey.Meta.CreateTime,
	}

	if ks.rsaPrivateKey != nil {
		pubKey.PublicKey = ks.rsaPKCS1PublicKey
		pubKey.Algorithm = structs.PubKeyAlgRS256
	} else {
		pubKey.PublicKey = ks.eddsaPrivateKey.Public().(ed25519.PublicKey)
		pubKey.Algorithm = structs.PubKeyAlgEdDSA
	}

	return pubKey, nil
}

// newKMSWrapper returns a go-kms-wrapping interface the caller can use to
// encrypt the RootKey with a key encryption key (KEK).
func (e *Encrypter) newKMSWrapper(provider *structs.KEKProviderConfig, keyID string, kek []byte) (kms.Wrapper, error) {
	var wrapper kms.Wrapper

	// note: adding support for another provider from go-kms-wrapping is a
	// matter of adding the dependency and another case here, but the remaining
	// third-party providers add significantly to binary size

	switch provider.Provider {
	case structs.KEKProviderAWSKMS:
		wrapper = awskms.NewWrapper()
	case structs.KEKProviderAzureKeyVault:
		wrapper = azurekeyvault.NewWrapper()
	case structs.KEKProviderGCPCloudKMS:
		wrapper = gcpckms.NewWrapper()
	case structs.KEKProviderVaultTransit:
		wrapper = transit.NewWrapper()

	default: // "aead"
		wrapper := aead.NewWrapper()
		wrapper.SetConfig(context.Background(),
			aead.WithAeadType(kms.AeadTypeAesGcm),
			aead.WithHashType(kms.HashTypeSha256),
			kms.WithKeyId(keyID),
		)
		err := wrapper.SetAesGcmKeyBytes(kek)
		if err != nil {
			return nil, err
		}
		return wrapper, nil
	}

	config, ok := e.providerConfigs[provider.ID()]
	if ok {
		_, err := wrapper.SetConfig(context.Background(), kms.WithConfigMap(config.Config))
		if err != nil {
			return nil, err
		}
	}
	return wrapper, nil
}

type KeyringReplicator struct {
	srv       *Server
	encrypter *Encrypter
	logger    hclog.Logger
	stopFn    context.CancelFunc
}

func NewKeyringReplicator(srv *Server, e *Encrypter) *KeyringReplicator {
	ctx, cancel := context.WithCancel(context.Background())
	repl := &KeyringReplicator{
		srv:       srv,
		encrypter: e,
		logger:    srv.logger.Named("keyring.replicator"),
		stopFn:    cancel,
	}
	go repl.run(ctx)
	return repl
}

// stop is provided for testing
func (krr *KeyringReplicator) stop() {
	krr.stopFn()
}

const keyringReplicationRate = 5

func (krr *KeyringReplicator) run(ctx context.Context) {
	krr.logger.Debug("starting encryption key replication")
	defer krr.logger.Debug("exiting key replication")

	limiter := rate.NewLimiter(keyringReplicationRate, keyringReplicationRate)

	for {
		select {
		case <-krr.srv.shutdownCtx.Done():
			return
		case <-ctx.Done():
			return
		default:
			err := limiter.Wait(ctx)
			if err != nil {
				continue // rate limit exceeded
			}

			store := krr.srv.fsm.State()
			iter, err := store.RootKeyMetas(nil)
			if err != nil {
				krr.logger.Error("failed to fetch keyring", "error", err)
				continue
			}
			for {
				raw := iter.Next()
				if raw == nil {
					break
				}

				keyMeta := raw.(*structs.RootKeyMeta)
				if key, err := krr.encrypter.GetKey(keyMeta.KeyID); err == nil && len(key.Key) > 0 {
					// the key material is immutable so if we've already got it
					// we can move on to the next key
					continue
				}

				err := krr.srv.replicateKey(ctx, krr.logger, keyMeta)
				if err != nil {
					// don't break the loop on an error, as we want to make sure
					// we've replicated any keys we can. the rate limiter will
					// prevent this case from sending excessive RPCs
					krr.logger.Error(err.Error(), "key", keyMeta.KeyID)
				}

			}

		}
	}

}

// replicateKey replicates a single key from peer servers that was present in
// the state store but missing from the keyring. Returns an error only if no
// peers have this key.
func (srv *Server) replicateKey(ctx context.Context, log hclog.Logger, keyMeta *structs.RootKeyMeta) error {
	keyID := keyMeta.KeyID
	// TODO: drop to Debug
	log.Warn("replicating new key", "id", keyID)

	var err error
	getReq := &structs.KeyringGetRootKeyRequest{
		KeyID: keyID,
		QueryOptions: structs.QueryOptions{
			Region:        srv.config.Region,
			MinQueryIndex: keyMeta.ModifyIndex - 1,
		},
	}
	getResp := &structs.KeyringGetRootKeyResponse{}

	// Key replication needs to tolerate leadership flapping. If a key is
	// rotated during a leadership transition, it's possible that the new leader
	// has not yet replicated the key from the old leader before the transition.
	//
	// If *we* are the leader then we're likely in this scenario.  Because
	// Keyring.Get will block for up to 1s waiting for key material, it's
	// critical for liveness of leader establishment we never send this request
	// to ourselves
	_, leaderID := srv.raft.LeaderWithID()
	if leaderID != raft.ServerID(srv.GetConfig().NodeID) {
		err = srv.RPC("Keyring.Get", getReq, getResp)
	}

	if err != nil || getResp.Key == nil {
		// log.Warn("failed to fetch key from current leader, trying peers",
		// 	"key", keyID, "error", err)
		getReq.AllowStale = true
		var err error

		cfg := srv.GetConfig()
		self := fmt.Sprintf("%s.%s", cfg.NodeName, cfg.Region)

		for _, peer := range srv.getAllPeers() {
			if peer.Name == self {
				log.Warn("skipping self")
				continue
			}

			log.Warn("attempting to replicate key from peer", "id", keyID, "peer", peer.Name)
			err = srv.forwardServer(peer, "Keyring.Get", getReq, getResp)
			if err == nil && getResp.Key != nil {
				break
			}
		}
	}
	if getResp.Key == nil {
		log.Error("failed to fetch key from any peer",
			"key", keyID, "error", err)
		return fmt.Errorf("failed to fetch key from any peer: %v", err)
	}

	// TODO: DEBUG: remove this
	log.Warn("replicated new key, checking if cluster upgraded", "id", keyID)

	isClusterUpgraded := ServersMeetMinimumVersion(
		srv.serf.Members(), srv.Region(), minVersionKeyringInRaft, true)

	// In the legacy replication, we toss out the wrapped key because it's
	// always persisted to disk
	_, err = srv.encrypter.AddUnwrappedKey(getResp.Key, isClusterUpgraded)
	if err != nil {
		return fmt.Errorf("failed to add key to keyring: %v", err)
	}

	log.Debug("added key", "key", keyID)
	return nil
}

func (srv *Server) getAllPeers() []*serverParts {
	srv.peerLock.RLock()
	defer srv.peerLock.RUnlock()
	peers := make([]*serverParts, 0, len(srv.localPeers))
	for _, peer := range srv.localPeers {
		peers = append(peers, peer.Copy())
	}
	return peers
}

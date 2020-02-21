# go-licensing

Example
=======

```go
// Create a temporary license to bootstrap the license watcher
now := time.Now()
tempLicense := &licensing.License{
    LicenseID:      "test-license",
    IssueTime:      now,
    StartTime:      now,
    ExpirationTime: now.Add(30 * time.Minute),
    Product:        "test-product",
    InstallationID: "*",
    Flags: map[string]interface{}{
        "package": "premium",
    },
}

// Generate a new public/private key pair
publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
if err != nil {
    return nil, err
}

// Sign license using generated key pair
initLicense, err := tempLicense.SignedString(privateKey)
if err != nil {
    return nil, err
}

// Set up license watcher
watcherOptions := &licensing.WatcherOptions{
    ProductName:          "test-product",
    InitLicense:          initLicense,
    AdditionalPublicKeys: []string{publicKey},
}
watcher, _, err := licensing.NewWatcher(watcherOptions)
if err != nil {
    return err
}

// Wait for events
for {
    select {
    case license := <-watcher.UpdateCh():
        // Handle license update events
    case license := <-watcher.WarningCh():
        // Handle license warnings
    case err := <-watcher.ErrorCh():
        // Handle license errors and expirations
    }
}
```

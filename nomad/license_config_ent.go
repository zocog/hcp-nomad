//go:build ent

package nomad

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-licensing/v3"
	nomadLicense "github.com/hashicorp/nomad-licensing/license"
)

func (c *LicenseConfig) Validate() error {
	blob, err := c.licenseString()
	if err != nil {
		return err
	}
	validator, err := c.validator()
	if err != nil {
		return err
	}
	_, err = validator.Validate(blob)
	if err != nil {
		return fmt.Errorf("invalid license config: %w", err)
	}
	return nil
}

func (c *LicenseConfig) validator() (*licensing.Validator, error) {
	validatorOpts := licensing.ValidatorOptions{
		ProductName:    nomadLicense.ProductName,
		StringKeys:     c.AdditionalPubKeys,
		InstallationID: "",
		BuildDate:      c.BuildDate,
		ExtraValidation: func(l *licensing.License) error {
			_, err := nomadLicense.NewLicense(l)
			return err
		},
	}
	validator, err := licensing.NewValidatorFromOptions(validatorOpts)
	if err != nil {
		return nil, fmt.Errorf("error initializing licensing validator: %w", err)
	}
	return validator, nil
}

func (c *LicenseConfig) licenseString() (string, error) {
	if c.LicenseEnvBytes != "" {
		return c.LicenseEnvBytes, nil
	}

	if c.LicensePath != "" {
		licRaw, err := os.ReadFile(c.LicensePath)
		if err != nil {
			return "", fmt.Errorf("failed to read license file %w", err)
		}
		return strings.TrimRight(string(licRaw), "\r\n"), nil
	}

	blob, err := defaultEnterpriseLicense(c)
	if err != nil {
		return "", fmt.Errorf("failed to set default license %w", err)
	}
	return blob, nil
}

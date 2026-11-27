package ipc

import (
	"yaare/internal/adapters/recovery"
	"yaare/internal/adapters/secrets"
	"yaare/internal/adapters/storage/sqlite"
)

func DesktopCredentialProtector(store *sqlite.Store, root func() (string, error)) secrets.Protector {
	return workspaceProtector(store, root, secrets.CurrentUser{})
}

func workspaceProtector(store *sqlite.Store, root func() (string, error), base secrets.Protector) secrets.Protector {
	return secrets.Checked{Base: base, Check: func(p secrets.Protector) error {
		return store.VerifyCredentialKey(p, func() error {
			dir, err := root()
			if err != nil {
				return err
			}
			return recovery.VerifyLegacy(dir, p)
		})
	}}
}

func ConfigureCredentialProtection(keys *KeyService, identities *IdentityService, ssh *SSHService, p secrets.Protector) {
	keys.protector = p
	identities.protector = p
	ssh.credentialProtector = p
}

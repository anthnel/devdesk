package credentials

import (
	"log"

	"github.com/anthnel/devdesk/internal/config"
)

// MigrateLegacySecrets moves the plaintext secrets an earlier DevDesk left in a
// context's configuration file into the store, then deletes them from the file.
//
// It runs on load. Dropping the fields from the schema and ignoring whatever is
// in the file would leave every existing user's token on disk forever, which is
// the opposite of what §3.9 asks for.
//
// The returned lines describe what happened, in the user's words, and are empty
// when there was nothing to migrate — the normal case. A secret that could not
// be stored stays in the file: losing it would be worse than leaving it.
//
// Nothing moves to a selection that does not persist (DEVDESK-745): saving to
// memory always succeeds, so the plaintext would be deleted from the file and
// the secret lost when the process exits. The file is left untouched and the
// note says so.
func MigrateLegacySecrets(sel Selection, contextName string) []string {
	legacy, err := config.ReadLegacySecrets(contextName)
	if err != nil {
		log.Printf("ERROR [credentials/migrate] read legacy secrets for context %q: %v", contextName, err)
		return nil
	}
	if legacy.Empty() {
		return nil
	}
	if !sel.Persists() {
		log.Printf("credentials/migrate: legacy secrets of context %q left in the file: backend %s does not persist", contextName, sel.Backend)
		return []string{"The plaintext secrets stayed in the configuration file: no persistent secret store answered."}
	}

	store := sel.Storage
	where := sel.StoreName()
	var notes []string
	movedToken := false
	movedPassword := false

	if legacy.GitLabToken != "" {
		movedToken, notes = migrateOne(store, where, legacy.GitLabURL, legacy.GitLabToken, "GitLab token", notes)
	}
	if legacy.RegistryPassword != "" {
		movedPassword, notes = migrateOne(store, where, legacy.RegistryURL, legacy.RegistryPassword, "registry password", notes)
	}

	if movedToken || movedPassword {
		if err := config.RemoveLegacySecrets(contextName, movedToken, movedPassword); err != nil {
			log.Printf("ERROR [credentials/migrate] strip legacy secrets from context %q: %v", contextName, err)
			return append(notes, "The plaintext copy could not be removed from the configuration file — delete it by hand.")
		}
	}
	return notes
}

// migrateOne stores a single secret and reports whether the file copy may now
// be deleted: only once it reads back from the store.
//
// A secret with no URL is dropped rather than stored: the URL is the key it
// would be filed under, and DevDesk cannot use a token it has no host for. That
// makes it unusable plaintext, and keeping unusable plaintext on disk is the
// one outcome worth avoiding — so it goes, and the user is told it went.
func migrateOne(store Storage, where, url, secret, label string, notes []string) (bool, []string) {
	if url == "" {
		log.Printf("credentials/migrate: dropping %s with no URL to key it on", label)
		return true, append(notes, "A "+label+" was removed from the configuration file: it had no URL and could not be used.")
	}

	if err := store.Save(url, secret); err != nil {
		log.Printf("ERROR [credentials/migrate] store %s for %s: %v", label, url, err)
		return false, append(notes, "The "+label+" is still in plaintext in the configuration file: "+err.Error())
	}

	// A successful Save proves nothing: `git credential approve` exits 0
	// whatever its helpers keep. The file copy goes only once the store gives
	// the same secret back (DEVDESK-745). The values are never logged.
	if got, err := store.Load(url); err != nil || got != secret {
		log.Printf("ERROR [credentials/migrate] %s for %s did not read back from %s (err: %v)", label, url, where, err)
		return false, append(notes, "The "+label+" is still in plaintext in the configuration file: "+where+" accepted it but did not read it back.")
	}

	log.Printf("credentials/migrate: moved %s for %s out of the configuration file into %s", label, url, where)
	return true, append(notes, "The "+label+" moved out of the configuration file and into "+where+".")
}

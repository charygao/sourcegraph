package authutil

import (
	"log"

	"gopkg.in/inconshreveable/log15.v2"

	sgxcli "src.sourcegraph.com/sourcegraph/sgx/cli"
)

func init() {
	sgxcli.PostInit = append(sgxcli.PostInit, func() {
		_, err := sgxcli.Serve.AddGroup("Authentication", "Authentication", &ActiveFlags)
		if err != nil {
			log.Fatal(err)
		}
	})

	sgxcli.ServeInit = append(sgxcli.ServeInit, func() {
		if ActiveFlags.DeprecatedOAuth2AuthServer {
			log15.Warn("The --auth.oauth2-auth-server flag is DEPRECATED. The functionality is always enabled now; the flag is unnecessary.")
		}
	})
}

// Flags defines some command-line flags for this package.
type Flags struct {
	AllowAnonymousReaders bool `long:"auth.allow-anon-readers" description:"allow unauthenticated users to perform read operations (viewing repos, etc.)"`

	Source string `long:"auth.source" description:"source of authentication to use (none|local)" default:"local"`

	// DeprecatedOAuth2AuthServer was deprecated because the OAuth2
	// auth server is now always enabled.
	//
	// TODO(sqs): Remove this field after the next release when the
	// deployment scripts have been updated.
	DeprecatedOAuth2AuthServer bool `long:"auth.oauth2-auth-server" hidden:"yes"`

	AllowAllLogins bool `long:"auth.allow-all-logins" description:"do not check access permissions of a user at login."`

	DisableAccessControl bool `long:"auth.disable-access-control" description:"do not check access level of a user for write/admin operations"`

	MigrateMode bool `long:"migrate-mode" description:"allow inserting users with specified UID, when migrating user data from another server"`
}

// IsLocal returns true if users are stored and authenticated locally.
func (f Flags) IsLocal() bool {
	return f.Source == "local"
}

// HasUserAccounts returns a boolean value indicating whether user
// accounts are enabled. If they are disabled, generally no
// login/signup functionality should be displayed or exposed.
func (f Flags) HasUserAccounts() bool {
	return f.Source != "" && f.Source != "none"
}

// HasLogin returns whether logging in is enabled.
func (f Flags) HasLogin() bool { return f.HasUserAccounts() }

// HasSignup returns whether signing up is enabled.
func (f Flags) HasSignup() bool { return f.IsLocal() }

func (f Flags) HasAccessControl() bool { return !f.DisableAccessControl && f.HasUserAccounts() }

// ActiveFlags are the flag values passed from the command line, if
// we're running as a CLI.
var ActiveFlags Flags

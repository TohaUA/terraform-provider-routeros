package routeros

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const testSystemCertificatesAddress = "routeros_system_certificate.root_ca"

func TestAccSystemCertificatesTest_basic(t *testing.T) {
	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			var rootCa, caCrt, serverCrt, clientCrt, scepClient, unsignedCrt MikrotikItem

			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				CheckDestroy:      testCheckResourceDestroy("/certificate", "routeros_system_certificate"),
				Steps: []resource.TestStep{
					{
						Config: testAccSystemCertificatesConfig(),
						Check: resource.ComposeTestCheckFunc(
							testResourcePrimaryInstanceId(testSystemCertificatesAddress),
							// root_ca
							testCheckResourceExists(testSystemCertificatesAddress, "/certificate", &rootCa),
							testCheckMikrotikItemAttr(testSystemCertificatesAddress, &rootCa, "name", "Test-Root-CA"),
							testCheckMikrotikItemAttrSet(testSystemCertificatesAddress, &rootCa, "fingerprint"),
							resource.TestCheckResourceAttr(testSystemCertificatesAddress, "name", "Test-Root-CA"),
							resource.TestCheckResourceAttrSet(testSystemCertificatesAddress, "fingerprint"),
							// ca_crt
							testCheckResourceExists("routeros_system_certificate.ca_crt", "/certificate", &caCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.ca_crt", &caCrt, "name", "CA-Certificate"),
							resource.TestCheckResourceAttr("routeros_system_certificate.ca_crt", "name", "CA-Certificate"),
							// server_crt
							testCheckResourceExists("routeros_system_certificate.server_crt", "/certificate", &serverCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.server_crt", &serverCrt, "name", "Server-Certificate"),
							testCheckMikrotikItemAttr("routeros_system_certificate.server_crt", &serverCrt, "ca", "Test-Root-CA"),
							testCheckMikrotikItemAttrSet("routeros_system_certificate.server_crt", &serverCrt, "fingerprint"),
							resource.TestCheckResourceAttr("routeros_system_certificate.server_crt", "name", "Server-Certificate"),
							resource.TestCheckResourceAttr("routeros_system_certificate.server_crt", "ca", "Test-Root-CA"),
							resource.TestCheckResourceAttrSet("routeros_system_certificate.server_crt", "fingerprint"),
							// client_crt
							testCheckResourceExists("routeros_system_certificate.client_crt", "/certificate", &clientCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.client_crt", &clientCrt, "name", "Client-Certificate"),
							resource.TestCheckResourceAttr("routeros_system_certificate.client_crt", "name", "Client-Certificate"),
							// unsigned_crt
							testCheckResourceExists("routeros_system_certificate.unsigned_crt", "/certificate", &unsignedCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.unsigned_crt", &unsignedCrt, "name", "Unsigned-Certificate"),
							testCheckNoMikrotikItemAttr("routeros_system_certificate.unsigned_crt", &unsignedCrt, "ca"),
							testCheckNoMikrotikItemAttr("routeros_system_certificate.unsigned_crt", &unsignedCrt, "fingerprint"),
							resource.TestCheckResourceAttr("routeros_system_certificate.unsigned_crt", "name", "Unsigned-Certificate"),
							resource.TestCheckNoResourceAttr("routeros_system_certificate.unsigned_crt", "ca"),
							resource.TestCheckNoResourceAttr("routeros_system_certificate.unsigned_crt", "fingerprint"),
							// scep_client
							testCheckResourceExists("routeros_system_certificate.scep_client", "/certificate", &scepClient),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "name", "SCEP-Client"),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "common-name", "scep-client.crt"),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "key-usage", "digital-signature,key-agreement,tls-client"),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "scep-url", "http://scep.server/scep/test"),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "challenge-password", "12345"),
							testCheckMikrotikItemAttrSet("routeros_system_certificate.scep_client", &scepClient, "status"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "name", "SCEP-Client"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "common_name", "scep-client.crt"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "key_usage.#", "3"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "key_usage.0", "digital-signature"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "key_usage.1", "key-agreement"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "key_usage.2", "tls-client"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "scep_url", "http://scep.server/scep/test"),
							resource.TestCheckResourceAttr("routeros_system_certificate.scep_client", "challenge_password", "12345"),
							resource.TestCheckResourceAttrSet("routeros_system_certificate.scep_client", "status"),
							resource.TestCheckResourceAttrPair("routeros_system_certificate.scep_client", "scep_url", "routeros_system_certificate.scep_client", "sign_via_scep.0.scep_url"),
							resource.TestCheckResourceAttrPair("routeros_system_certificate.scep_client", "challenge_password", "routeros_system_certificate.scep_client", "sign_via_scep.0.challenge_password"),
						),
					},
					{
						Config: testAccSystemCertificatesConfigUpdated(),
						Check: resource.ComposeAggregateTestCheckFunc(
							// root_ca
							testCheckResourceExists(testSystemCertificatesAddress, "/certificate", &rootCa),
							testCheckMikrotikItemAttr(testSystemCertificatesAddress, &rootCa, "name", "Test-Root-CA"),
							// ca_crt (added ca_crl_host)
							testCheckResourceExists("routeros_system_certificate.ca_crt", "/certificate", &caCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.ca_crt", &caCrt, "name", "CA-Certificate"),
							testCheckMikrotikItemAttr("routeros_system_certificate.ca_crt", &caCrt, "ca-crl-host", "10.0.0.1"),
							resource.TestCheckResourceAttr("routeros_system_certificate.ca_crt", "name", "CA-Certificate"),
							resource.TestCheckResourceAttr("routeros_system_certificate.ca_crt", "ca_crl_host", "10.0.0.1"),
							// server_crt (changed name)
							testCheckResourceExists("routeros_system_certificate.server_crt", "/certificate", &serverCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.server_crt", &serverCrt, "name", "Server-Certificate-New"),
							testCheckMikrotikItemAttr("routeros_system_certificate.server_crt", &serverCrt, "ca", "Test-Root-CA"),
							resource.TestCheckResourceAttr("routeros_system_certificate.server_crt", "name", "Server-Certificate-New"),
							resource.TestCheckResourceAttr("routeros_system_certificate.server_crt", "ca", "Test-Root-CA"),
							// client_crt (added country, renamed to avoid 'name exists' error)
							testCheckResourceExists("routeros_system_certificate.client_crt", "/certificate", &clientCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.client_crt", &clientCrt, "name", "Client-Certificate-New"),
							testCheckMikrotikItemAttr("routeros_system_certificate.client_crt", &clientCrt, "country", "US"),
							resource.TestCheckResourceAttr("routeros_system_certificate.client_crt", "name", "Client-Certificate-New"),
							resource.TestCheckResourceAttr("routeros_system_certificate.client_crt", "country", "US"),
							// unsigned_crt (certificate requested to be signed)
							testCheckResourceExists("routeros_system_certificate.unsigned_crt", "/certificate", &unsignedCrt),
							testCheckMikrotikItemAttr("routeros_system_certificate.unsigned_crt", &unsignedCrt, "name", "Unsigned-Certificate"),
							testCheckMikrotikItemAttr("routeros_system_certificate.unsigned_crt", &unsignedCrt, "ca", "Test-Root-CA"),
							testCheckMikrotikItemAttrSet("routeros_system_certificate.unsigned_crt", &unsignedCrt, "fingerprint"),
							resource.TestCheckResourceAttr("routeros_system_certificate.unsigned_crt", "name", "Unsigned-Certificate"),
							resource.TestCheckResourceAttr("routeros_system_certificate.unsigned_crt", "ca", "Test-Root-CA"),
							resource.TestCheckResourceAttrSet("routeros_system_certificate.unsigned_crt", "fingerprint"),
							// scep_client (changed scep_url)
							testCheckResourceExists("routeros_system_certificate.scep_client", "/certificate", &scepClient),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "name", "SCEP-Client"),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "common-name", "scep-client.crt"),
							testCheckMikrotikItemAttr("routeros_system_certificate.scep_client", &scepClient, "scep-url", "http://scep.server/scep/test_updated"),
						),
					},
				},
			})

		})
	}
}

func testAccSystemCertificatesConfig() string {
	return providerConfig + `

resource "routeros_system_certificate" "root_ca" {
	name        = "Test-Root-CA"
	common_name = "RootCA"
	key_usage   = ["key-cert-sign", "crl-sign"]
	trusted     = true
	sign {
	}
}	  

resource "routeros_system_certificate" "ca_crt" {
	name        = "CA-Certificate"
	common_name = "ca.crt"
	key_usage   = ["key-cert-sign", "crl-sign"]
	sign {
	}
}

resource "routeros_system_certificate" "server_crt" {
	name        = "Server-Certificate"
	common_name = "server.crt"
	// KUs: igitalSignature, keyEncipherment or keyAgreement
	key_usage   = ["digital-signature", "key-encipherment", "tls-server"]
	sign {
		ca = routeros_system_certificate.root_ca.name
	}
}

resource "routeros_system_certificate" "client_crt" {
	name        = "Client-Certificate"
	common_name = "client.crt"
	key_size    = "prime256v1"
	// KUs: digitalSignature and/or keyAgreement
	key_usage   = ["digital-signature", "key-agreement", "tls-client"]
	sign {
		ca = routeros_system_certificate.root_ca.name
	}
}

resource "routeros_system_certificate" "unsigned_crt" {
	name             = "Unsigned-Certificate"
	common_name      = "unsigned.crt"
	key_size         = "1024"
	subject_alt_name = "DNS:router.lan,DNS:myrouter.lan,IP:192.168.88.1"
}

resource "routeros_system_certificate" "scep_client" {
  name        = "SCEP-Client"
  common_name = "scep-client.crt"
  key_usage   = ["digital-signature", "key-agreement", "tls-client"]

  sign_via_scep {
    scep_url           = "http://scep.server/scep/test"
    challenge_password = "12345"
  }
}
`
}

func testAccSystemCertificatesConfigUpdated() string {
	return providerConfig + `

resource "routeros_system_certificate" "root_ca" {
  name        = "Test-Root-CA"
  common_name = "RootCA"
  key_usage   = ["key-cert-sign", "crl-sign"]
  trusted     = true
  sign {
  }
}

# Added ca_crl_host
resource "routeros_system_certificate" "ca_crt" {
  name        = "CA-Certificate"
  common_name = "ca.crt"
  key_usage   = ["key-cert-sign", "crl-sign"]
  sign {
    ca_crl_host = "10.0.0.1"
  }
}

# Changed name
resource "routeros_system_certificate" "server_crt" {
  name        = "Server-Certificate-New"
  common_name = "server.crt"
  // KUs: igitalSignature, keyEncipherment or keyAgreement
  key_usage   = ["digital-signature", "key-encipherment", "tls-server"]
  sign {
    ca = routeros_system_certificate.root_ca.name
  }
}

# Added country, have to change a name
resource "routeros_system_certificate" "client_crt" {
  name        = "Client-Certificate-New"
  common_name = "client.crt"
  country     = "US"
  key_size    = "prime256v1"
  // KUs: digitalSignature and/or keyAgreement
  key_usage   = ["digital-signature", "key-agreement", "tls-client"]
  sign {
    ca = routeros_system_certificate.root_ca.name
  }
}

# Sign unsigned certificate
resource "routeros_system_certificate" "unsigned_crt" {
  name             = "Unsigned-Certificate"
  common_name      = "unsigned.crt"
  key_size         = "1024"
  subject_alt_name = "DNS:router.lan,DNS:myrouter.lan,IP:192.168.88.1"
  sign {
    ca = routeros_system_certificate.root_ca.name
  }
}

# Changed scep_url
resource "routeros_system_certificate" "scep_client" {
  name        = "SCEP-Client"
  common_name = "scep-client.crt"
  key_usage   = ["digital-signature", "key-agreement", "tls-client"]

  sign_via_scep {
    scep_url           = "http://scep.server/scep/test_updated"
    challenge_password = "12345"
  }
}
`
}

const testSystemCertificateKeyCleanupName = "key-cleanup.crt"

var uuidFileName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// An import from key_file_content uploads the private key to a UUID-named file.
// RouterOS removes it only after a successful import, so a failed import must not
// leave the key in the file list.
func TestAccSystemCertificate_importKeyFailureCleansUp(t *testing.T) {
	// ROS 7.12.x does not return IDs for created files.
	if !testCheckMinVersion(t, testFileMinVersion) {
		t.Logf("Test skipped, the minimum required version is %v", testFileMinVersion)
		return
	}

	for _, name := range testNames {
		t.Run(name, func(t *testing.T) {
			resource.Test(t, resource.TestCase{
				PreCheck: func() {
					testAccPreCheck(t)
					testSetTransportEnv(t, name)
				},
				ProviderFactories: testAccProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: testAccSystemCertificateKeyCleanupConfig(),
						// The device declines the import: REST names the import URL,
						// the API reports a device error.
						ExpectError: regexp.MustCompile(`certificate/import|from RouterOS device`),
					},
					{
						// The failed create leaves nothing in the state, so an empty
						// configuration is enough to run the device check.
						Config: providerConfig,
						Check:  testCheckNoLeftoverKeyFile(),
					},
				},
			})
		})
	}
}

// testCheckNoLeftoverKeyFile fails when a UUID-named upload is still in /file. The
// certificate half of the failed import may have landed, so it is removed here to
// keep the device clean for the other tests.
func testCheckNoLeftoverKeyFile() resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client := testAccProvider.Meta().(Client)

		certs, err := ReadItemsFiltered([]string{"name=" + testSystemCertificateKeyCleanupName}, "/certificate", client)
		if err != nil {
			return err
		}
		for _, c := range *certs {
			if err := DeleteItem(&ItemId{Id, c.GetID(Id)}, "/certificate", client); err != nil {
				return err
			}
		}

		files, err := ReadItems(nil, "/file", client)
		if err != nil {
			return err
		}
		for _, f := range *files {
			if uuidFileName.MatchString(f["name"]) {
				return fmt.Errorf("the uploaded file %q is still on the router after the failed import", f["name"])
			}
		}
		return nil
	}
}

func testAccSystemCertificateKeyCleanupConfig() string {
	return providerConfig + `
resource "routeros_system_certificate" "key_cleanup" {
  name        = "` + testSystemCertificateKeyCleanupName + `"
  common_name = "External Certificate"
  import {
    cert_file_content = <<EOT
-----BEGIN CERTIFICATE-----
MIIBlTCCATugAwIBAgIINLsws71B5zIwCgYIKoZIzj0EAwIwHzEdMBsGA1UEAwwU
RXh0ZXJuYWwgQ2VydGlmaWNhdGUwHhcNMjQwNTE3MjEyOTUzWhcNMjUwNTE3MjEy
OTUzWjAfMR0wGwYDVQQDDBRFeHRlcm5hbCBDZXJ0aWZpY2F0ZTBZMBMGByqGSM49
AgEGCCqGSM49AwEHA0IABKE1g0Qj4ujIold9tklu2z4BUu/K7xDFF5YmedtOfJyM
1/80APNboqn71y4m4XNE1JNtQuR2bSZPHVrzODkR16ujYTBfMA8GA1UdEwEB/wQF
MAMBAf8wDgYDVR0PAQH/BAQDAgG2MB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEF
BQcDAjAdBgNVHQ4EFgQUNXd5bvluIV9YAhGc5yMHc6OzXpMwCgYIKoZIzj0EAwID
SAAwRQIhAODte/qS6CE30cvnQpxP/ObWBPIPZnHtkFHIIC1AOSXwAiBGCGQE+aJY
W72Rw0Y1ckvlt6sU0urkzGuj5wxVF/gSYA==
-----END CERTIFICATE-----
EOT
    # Not a key: the key import has to fail.
    key_file_content = "this is not a private key"
    passphrase       = "11111111"
  }
}
`
}

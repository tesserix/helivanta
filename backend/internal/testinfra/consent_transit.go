package testinfra

// ClinicalConsentTransitPolicy is exercised against real OpenBao. Production policy
// installation is a separate GitOps activation gate, not implied by this fixture.
const ClinicalConsentTransitPolicy = `path "transit/keys/helivanta-consent-*" {
 capabilities = ["create","update"]
 required_parameters = ["type","key_size","exportable","allow_plaintext_backup"]
 allowed_parameters = { "type" = ["hmac"], "key_size" = ["32"], "exportable" = [false], "allow_plaintext_backup" = [false] }
}
path "transit/keys/+/*" { capabilities = ["deny"] }
path "transit/hmac/helivanta-consent-*" { capabilities = ["update"] }`

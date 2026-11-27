resource "routeros_ppp_secret" "test" {
  name                = "user-test"
  password            = "123"
  password_wo_version = 1
  profile             = "default"
}

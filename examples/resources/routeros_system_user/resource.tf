resource "routeros_system_user" "test" {
  name                = "test-user-1"
  address             = "0.0.0.0/0"
  group               = "read"
  password            = "secret"
  password_wo_version = 1
  comment             = "Test User"
}

data "routeros_wifi_easy_connect" "test" {
  type     = "WPA2"
  ssid     = "test"
  password = "password12345"
}

# The QR code embeds the Wi-Fi password, so the output must be sensitive.
# Print it with: terraform output -raw qrcode
output "qrcode" {
  value     = data.routeros_wifi_easy_connect.test.qr_code
  sensitive = true
}

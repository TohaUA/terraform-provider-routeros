# Filter by name: without a filter the data source reads every file on the
# router, including the contents of each one.
data "routeros_files" "files" {
  filter = {
    name = "file.txt"
  }
}

#The ID can be found via API or the terminal
#The command for the terminal is -> :put [/system/package/local-update/update-package-source get [print show-ids]]
terraform import routeros_system_package_local_update_source.hub *1
#Or you can import a resource using one of its attributes
terraform import routeros_system_package_local_update_source.hub "address=192.168.88.1"

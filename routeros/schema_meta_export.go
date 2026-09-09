package routeros

// Exported accessors for the schema metadata parsers used by the serializer.
// They exist so that tools inside this module (tools/schema-drift) apply exactly
// the same field-name transformations the provider applies at runtime instead of
// re-implementing them.

// MetaTransformSetMap parses a MetaTransformSet default value ("tf_name: mt-name", ...).
// reverse=false returns TF -> MT, reverse=true returns MT -> TF.
func MetaTransformSetMap(s string, reverse bool) map[string]string {
	return loadTransformSet(s, reverse)
}

// MetaSkipFieldsSet parses a MetaSkipFields / MetaSetUnsetFields / MetaDropByValue default value.
func MetaSkipFieldsSet(s string) map[string]struct{} {
	return loadSkipFields(s)
}

// ResourceDriftMap returns the version-dependent attribute renames registered for a resource path
// (see mikrotik_resource_drift.go). reverse=false returns TF -> MT, reverse=true returns MT -> TF.
func ResourceDriftMap(ros, path string, reverse bool) (map[string]string, error) {
	if _, err := parseRouterOSVersion(ros); err != nil {
		return nil, err
	}
	return driftAttributeSlice.GetDriftMap(ros, path, reverse), nil
}

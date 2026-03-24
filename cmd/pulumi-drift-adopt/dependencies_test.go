// Copyright 2026, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDependencyResolution(t *testing.T) {
	// Load test fixtures
	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)

	steps, _, err := parsePreviewOutput(eventsData)
	require.NoError(t, err)

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	depMap := buildDepMapFromState(stateLookup)
	resources := convertStepsToResources(steps, depMap)
	require.Len(t, resources, 2)

	// Find the ca-cert resource (SelfSignedCert)
	var caCert *ResourceChange
	var serverCert *ResourceChange
	for i := range resources {
		switch resources[i].Name {
		case "ca-cert":
			caCert = &resources[i]
		case "server-cert":
			serverCert = &resources[i]
		}
	}
	require.NotNil(t, caCert, "ca-cert resource not found")
	require.NotNil(t, serverCert, "server-cert resource not found")

	// ca-cert.privateKeyPem should have dependsOn metadata (no literal value)
	pkPem := caCert.InputProperties["privateKeyPem"]
	pkMap, ok := pkPem.(map[string]interface{})
	require.True(t, ok, "privateKeyPem should be a map with dependsOn, got %T", pkPem)
	assert.Nil(t, pkMap["value"], "value should be omitted when dependsOn is present")

	depInfo, ok := pkMap["dependsOn"].(map[string]interface{})
	require.True(t, ok, "dependsOn should be a map")
	assert.Equal(t, "ca-key", depInfo["resourceName"])
	assert.Equal(t, "tls:index/privateKey:PrivateKey", depInfo["resourceType"])
	assert.Equal(t, "privateKeyPem", depInfo["outputProperty"])

	// ca-cert.validityPeriodHours should be a plain value (no deps)
	assert.Equal(t, float64(87600), caCert.InputProperties["validityPeriodHours"])

	// server-cert.caPrivateKeyPem should resolve to ca-key
	caKeyPem := serverCert.InputProperties["caPrivateKeyPem"]
	caKeyMap, ok := caKeyPem.(map[string]interface{})
	require.True(t, ok, "caPrivateKeyPem should be a map with dependsOn")
	caKeyDep, ok := caKeyMap["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-key", caKeyDep["resourceName"])
	assert.Equal(t, "privateKeyPem", caKeyDep["outputProperty"])

	// server-cert.caCertPem should resolve to ca-cert
	caCertPem := serverCert.InputProperties["caCertPem"]
	caCertMap, ok := caCertPem.(map[string]interface{})
	require.True(t, ok, "caCertPem should be a map with dependsOn")
	caCertDep, ok := caCertMap["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-cert", caCertDep["resourceName"])
	assert.Equal(t, "tls:index/selfSignedCert:SelfSignedCert", caCertDep["resourceType"])
	assert.Equal(t, "certPem", caCertDep["outputProperty"])

	// server-cert.certRequestPem has empty deps — should be plain value
	_, isCsrMap := serverCert.InputProperties["certRequestPem"].(map[string]interface{})
	assert.False(t, isCsrMap, "certRequestPem has empty deps, should be plain value")
}

func TestDependencyResolutionEdgeCases(t *testing.T) {
	t.Run("no state lookup - returns plain values", func(t *testing.T) {
		eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
		require.NoError(t, err)

		steps, _, err := parsePreviewOutput(eventsData)
		require.NoError(t, err)

		// nil stateLookup — should return plain values
		resources := convertStepsToResources(steps, nil)
		require.NotEmpty(t, resources)

		for _, res := range resources {
			if res.Action != "add_to_code" {
				continue
			}
			for key, val := range res.InputProperties {
				_, isMap := val.(map[string]interface{})
				if isMap {
					// Should NOT have dependsOn when no state lookup
					m := val.(map[string]interface{})
					_, hasDep := m["dependsOn"]
					assert.False(t, hasDep, "property %s should not have dependsOn without state lookup", key)
				}
			}
		}
	})

	t.Run("dep URN not in state - returns plain value", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::orphan-cert",
				"oldState": {
					"type": "tls:index/selfSignedCert:SelfSignedCert",
					"inputs": {"privateKeyPem": "some-pem-value"},
					"propertyDependencies": {
						"privateKeyPem": ["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::missing-key"]
					}
				}
			}]
		}`
		stateContent := `{"version": 3, "deployment": {"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"}, "resources": []}}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		// Should be plain string since dep URN is missing from state
		assert.Equal(t, "some-pem-value", resources[0].InputProperties["privateKeyPem"])
	})

	t.Run("engine events format with propertyDependencies", func(t *testing.T) {
		// NDJSON engine events format: "old" (not "oldState"), "diffKind" (not "kind")
		ndjsonContent := `{"type":"resourcePreEvent","resourcePreEvent":{"metadata":{"op":"delete","urn":"urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::ndjson-cert","old":{"type":"tls:index/selfSignedCert:SelfSignedCert","inputs":{"privateKeyPem":"ndjson-pem-value","validityPeriodHours":8760},"outputs":{"certPem":"ndjson-cert-pem","privateKeyPem":"ndjson-pem-value"},"propertyDependencies":{"privateKeyPem":["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ndjson-key"]}}}}}
{"type":"resourcePreEvent","resourcePreEvent":{"metadata":{"op":"delete","urn":"urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ndjson-key","old":{"type":"tls:index/privateKey:PrivateKey","inputs":{"algorithm":"RSA"},"outputs":{"algorithm":"RSA","privateKeyPem":"ndjson-pem-value"}}}}}`

		steps, _, err := parsePreviewOutput([]byte(ndjsonContent))
		require.NoError(t, err)
		require.Len(t, steps, 2)

		// Build lookup from steps (no external state)
		stateLookup := buildStateLookupFromSteps(steps)
		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))

		var cert *ResourceChange
		for i := range resources {
			if resources[i].Name == "ndjson-cert" {
				cert = &resources[i]
			}
		}
		require.NotNil(t, cert)

		// PropertyDependencies should survive NDJSON parsing and resolve
		pkPem, ok := cert.InputProperties["privateKeyPem"].(map[string]interface{})
		require.True(t, ok, "privateKeyPem should have dependsOn from engine events format")
		dep, ok := pkPem["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "ndjson-key", dep["resourceName"])
		assert.Equal(t, "privateKeyPem", dep["outputProperty"])
	})

	t.Run("value not found in dep outputs - bare dependsOn with single depURN", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::no-match-cert",
				"oldState": {
					"type": "tls:index/selfSignedCert:SelfSignedCert",
					"inputs": {"privateKeyPem": "value-that-doesnt-match-any-output"},
					"propertyDependencies": {
						"privateKeyPem": ["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::some-key"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::some-key",
					"type": "tls:index/privateKey:PrivateKey",
					"inputs": {"algorithm": "RSA"},
					"outputs": {"privateKeyPem": "different-pem-value", "algorithm": "RSA"}
				}]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)
		for urn, res := range buildStateLookupFromSteps(steps) {
			if _, exists := stateLookup[urn]; !exists {
				stateLookup[urn] = res
			}
		}

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		// Value doesn't match any output, but single depURN → bare dependsOn (no outputProperty)
		pkPem, ok := resources[0].InputProperties["privateKeyPem"].(map[string]interface{})
		require.True(t, ok, "privateKeyPem should have bare dependsOn, got %T", resources[0].InputProperties["privateKeyPem"])
		dep, ok := pkPem["dependsOn"].(map[string]interface{})
		require.True(t, ok, "dependsOn should be a map")
		assert.Equal(t, "some-key", dep["resourceName"])
		assert.Equal(t, "tls:index/privateKey:PrivateKey", dep["resourceType"])
		assert.Nil(t, dep["outputProperty"], "bare dependsOn should not have outputProperty")
	})

	t.Run("plaintext secret output matches correctly (show-secrets)", func(t *testing.T) {
		// With --show-secrets, secret outputs appear as plaintext — no sentinel wrapper.
		// Each array element is resolved individually: "secret-password-value" matches
		// the "result" output, so triggers becomes [{dependsOn: {outputProperty: "result"}}].
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::deploy-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["secret-password-value"]},
					"propertyDependencies": {
						"triggers": ["urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass",
					"type": "random:index/randomPassword:RandomPassword",
					"inputs": {"length": 16},
					"outputs": {"result": "secret-password-value", "length": 16}
				}]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)
		for urn, res := range buildStateLookupFromSteps(steps) {
			if _, exists := stateLookup[urn]; !exists {
				stateLookup[urn] = res
			}
		}

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		// Array elements are resolved individually: "secret-password-value" matches result output.
		// triggers should be an array with one dependsOn element (with outputProperty).
		triggersRaw := resources[0].InputProperties["triggers"]
		triggers, ok := triggersRaw.([]interface{})
		require.True(t, ok, "triggers should be an array, got %T", triggersRaw)
		require.Len(t, triggers, 1)
		elem, ok := triggers[0].(map[string]interface{})
		require.True(t, ok, "trigger element should be a map")
		dep, ok := elem["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "api-pass", dep["resourceName"])
		assert.Equal(t, "random:index/randomPassword:RandomPassword", dep["resourceType"])
		assert.Equal(t, "result", dep["outputProperty"])
	})

	t.Run("array element matched to string output - resolves outputProperty", func(t *testing.T) {
		// Input is ["hex-output-value"] (array), output has "hex": "hex-output-value" (string).
		// With element-level resolution, the element matches the "hex" output directly.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::worker-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["hex-output-value"]},
					"propertyDependencies": {
						"triggers": ["urn:pulumi:dev::test::random:index/randomId:RandomId::worker-id"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomId:RandomId::worker-id",
					"type": "random:index/randomId:RandomId",
					"inputs": {"byteLength": 8},
					"outputs": {"hex": "hex-output-value", "b64Std": "base64value"}
				}]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)
		for urn, res := range buildStateLookupFromSteps(steps) {
			if _, exists := stateLookup[urn]; !exists {
				stateLookup[urn] = res
			}
		}

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		// Array element "hex-output-value" matches output "hex" → resolved with outputProperty
		triggersRaw := resources[0].InputProperties["triggers"]
		triggers, ok := triggersRaw.([]interface{})
		require.True(t, ok, "triggers should be an array, got %T", triggersRaw)
		require.Len(t, triggers, 1)
		elem, ok := triggers[0].(map[string]interface{})
		require.True(t, ok, "trigger element should be a map")
		dep, ok := elem["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "worker-id", dep["resourceName"])
		assert.Equal(t, "random:index/randomId:RandomId", dep["resourceType"])
		assert.Equal(t, "hex", dep["outputProperty"])
	})

	t.Run("multiple depURNs with no match returns plain value", func(t *testing.T) {
		// When there are multiple depURNs and no exact match, we can't emit bare dependsOn
		// because it would be ambiguous which resource to reference.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::multi-dep-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["ambiguous-value"]},
					"propertyDependencies": {
						"triggers": [
							"urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-a",
							"urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-b"
						]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [
					{
						"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-a",
						"type": "random:index/randomPassword:RandomPassword",
						"inputs": {"length": 16},
						"outputs": {"result": "different-a", "length": 16}
					},
					{
						"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-b",
						"type": "random:index/randomPassword:RandomPassword",
						"inputs": {"length": 16},
						"outputs": {"result": "different-b", "length": 16}
					}
				]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		// Multiple depURNs, no exact match → plain value (no bare dependsOn due to ambiguity)
		triggers := resources[0].InputProperties["triggers"]
		triggerArr, ok := triggers.([]interface{})
		require.True(t, ok, "triggers should be plain array value, got %T", triggers)
		assert.Equal(t, "ambiguous-value", triggerArr[0])
	})
}

func TestRunNextDepMapFileFlag(t *testing.T) {
	// Build dep map from state fixture
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)
	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)
	depMap := buildDepMapFromState(stateLookup)

	tmpDir := t.TempDir()
	depMapPath := filepath.Join(tmpDir, "depmap.json")
	depMapPath, err = saveDepMap(depMap)
	require.NoError(t, err)

	eventsFile := filepath.Join(tmpDir, "events.json")
	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(eventsFile, eventsData, 0644))

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--dep-map-file", depMapPath})

	assert.Equal(t, "changes_needed", summary.Status)

	var caCert *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "ca-cert" {
			caCert = &full.Resources[i]
		}
	}
	require.NotNil(t, caCert)

	pkPem, ok := caCert.InputProperties["privateKeyPem"].(map[string]interface{})
	require.True(t, ok, "privateKeyPem should have dependsOn metadata")
	dep, ok := pkPem["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-key", dep["resourceName"])
}

func TestDependencyResolutionFromPreviewOnly(t *testing.T) {
	// Preview events where OldState has PropertyDependencies AND
	// another step has the dependent resource with outputs
	eventsContent := `{
		"steps": [
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::inline-key",
				"oldState": {
					"type": "tls:index/privateKey:PrivateKey",
					"inputs": {"algorithm": "RSA", "rsaBits": 4096},
					"outputs": {
						"algorithm": "RSA",
						"privateKeyPem": "-----BEGIN RSA PRIVATE KEY-----\ninline-key-pem\n-----END RSA PRIVATE KEY-----\n"
					}
				}
			},
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::inline-cert",
				"oldState": {
					"type": "tls:index/selfSignedCert:SelfSignedCert",
					"inputs": {
						"privateKeyPem": "-----BEGIN RSA PRIVATE KEY-----\ninline-key-pem\n-----END RSA PRIVATE KEY-----\n",
						"validityPeriodHours": 8760
					},
					"propertyDependencies": {
						"privateKeyPem": ["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::inline-key"]
					}
				}
			}
		]
	}`

	steps, _, err := parsePreviewOutput([]byte(eventsContent))
	require.NoError(t, err)

	// Build state lookup from the preview steps themselves (no external state file)
	stateLookup := buildStateLookupFromSteps(steps)

	resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))

	var cert *ResourceChange
	for i := range resources {
		if resources[i].Name == "inline-cert" {
			cert = &resources[i]
		}
	}
	require.NotNil(t, cert)

	pkPem, ok := cert.InputProperties["privateKeyPem"].(map[string]interface{})
	require.True(t, ok, "should have dependsOn")
	dep := pkPem["dependsOn"].(map[string]interface{})
	assert.Equal(t, "inline-key", dep["resourceName"])
	assert.Equal(t, "privateKeyPem", dep["outputProperty"])
}

func TestDepMapFileInOutput(t *testing.T) {
	// Build dep map from state fixture and provide via --dep-map-file
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)
	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)
	depMap := buildDepMapFromState(stateLookup)

	tmpDir := t.TempDir()
	depMapPath, err := saveDepMap(depMap)
	require.NoError(t, err)

	eventsFile := filepath.Join(tmpDir, "events.json")
	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(eventsFile, eventsData, 0644))

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--dep-map-file", depMapPath})

	assert.NotEmpty(t, summary.DepMapFile, "depMapFile should be populated")
	assert.NotEmpty(t, full.DepMapFile, "depMapFile should also be in output file")
}

// TestNestedDependsOnMapProperty verifies that map-valued properties with PropertyDependencies
// get element-level dependsOn resolution, preserving map keys.
func TestNestedDependsOnMapProperty(t *testing.T) {
	t.Run("map value matches dep output - key preserved with outputProperty", func(t *testing.T) {
		// keepers = {"ref": "pet-name-val"}, dep resource has outputs {"id": "pet-name-val"}
		// Each map value is resolved individually: "pet-name-val" matches output "id".
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass",
				"oldState": {
					"type": "random:index/randomPassword:RandomPassword",
					"inputs": {"keepers": {"ref": "pet-name-val"}},
					"propertyDependencies": {
						"keepers": ["urn:pulumi:dev::test::random:index/randomPet:RandomPet::cache-pet"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomPet:RandomPet::cache-pet",
					"type": "random:index/randomPet:RandomPet",
					"inputs": {},
					"outputs": {"id": "pet-name-val", "separator": "-"}
				}]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)
		for urn, res := range buildStateLookupFromSteps(steps) {
			if _, exists := stateLookup[urn]; !exists {
				stateLookup[urn] = res
			}
		}

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		// keepers should be a map with preserved keys and resolved dependsOn per value
		keepersRaw := resources[0].InputProperties["keepers"]
		keepers, ok := keepersRaw.(map[string]interface{})
		require.True(t, ok, "keepers should be a map, got %T", keepersRaw)

		// Key "ref" should be preserved; its value should be a dependsOn wrapper
		refVal, ok := keepers["ref"].(map[string]interface{})
		require.True(t, ok, "keepers[\"ref\"] should be a dependsOn map, got %T", keepers["ref"])
		dep, ok := refVal["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "cache-pet", dep["resourceName"])
		assert.Equal(t, "random:index/randomPet:RandomPet", dep["resourceType"])
		assert.Equal(t, "id", dep["outputProperty"])
	})

	t.Run("map value encrypted - key preserved with bare dependsOn", func(t *testing.T) {
		// keepers = {"ref": "<encrypted>"}, dep resource has no matching output.
		// Map key preserved, bare dependsOn emitted for the value.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass",
				"oldState": {
					"type": "random:index/randomPassword:RandomPassword",
					"inputs": {"keepers": {"ref": "<encrypted>"}},
					"propertyDependencies": {
						"keepers": ["urn:pulumi:dev::test::random:index/randomPet:RandomPet::cache-pet"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomPet:RandomPet::cache-pet",
					"type": "random:index/randomPet:RandomPet",
					"inputs": {},
					"outputs": {"id": "pet-name-val"}
				}]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)
		for urn, res := range buildStateLookupFromSteps(steps) {
			if _, exists := stateLookup[urn]; !exists {
				stateLookup[urn] = res
			}
		}

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		keepersRaw := resources[0].InputProperties["keepers"]
		keepers, ok := keepersRaw.(map[string]interface{})
		require.True(t, ok, "keepers should be a map, got %T", keepersRaw)

		// Key "ref" preserved; value is bare dependsOn (no outputProperty)
		refVal, ok := keepers["ref"].(map[string]interface{})
		require.True(t, ok, "keepers[\"ref\"] should be a dependsOn map")
		dep, ok := refVal["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "cache-pet", dep["resourceName"])
		assert.Equal(t, "random:index/randomPet:RandomPet", dep["resourceType"])
		assert.Nil(t, dep["outputProperty"], "bare dependsOn should have no outputProperty")
	})

	t.Run("array element encrypted - array structure preserved with bare dependsOn", func(t *testing.T) {
		// triggers = ["<encrypted>"], dep has no matching output.
		// Array structure preserved; element becomes bare dependsOn.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::deploy-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["<encrypted>"]},
					"propertyDependencies": {
						"triggers": ["urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass",
					"type": "random:index/randomPassword:RandomPassword",
					"inputs": {"length": 16},
					"outputs": {
						"4dabf18193072939515e22adb298388d": "1b47061264138c4ac30d75fd1eb44270",
						"ciphertext": "abc123encrypted"
					}
				}]
			}
		}`

		steps, _, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)
		for urn, res := range buildStateLookupFromSteps(steps) {
			if _, exists := stateLookup[urn]; !exists {
				stateLookup[urn] = res
			}
		}

		resources := convertStepsToResources(steps, buildDepMapFromState(stateLookup))
		require.Len(t, resources, 1)

		triggersRaw := resources[0].InputProperties["triggers"]
		triggers, ok := triggersRaw.([]interface{})
		require.True(t, ok, "triggers should be an array, got %T", triggersRaw)
		require.Len(t, triggers, 1)

		// Element becomes bare dependsOn (encrypted = no output match)
		elem, ok := triggers[0].(map[string]interface{})
		require.True(t, ok, "trigger element should be a dependsOn map")
		dep, ok := elem["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "api-pass", dep["resourceName"])
		assert.Equal(t, "random:index/randomPassword:RandomPassword", dep["resourceType"])
		assert.Nil(t, dep["outputProperty"], "bare dependsOn should have no outputProperty")
	})
}

// TestSortResourcesByDependencies verifies topological sorting and DependencyLevel assignment.
func TestSortResourcesByDependencies(t *testing.T) {
	makeRes := func(name string, inputProps map[string]interface{}) ResourceChange {
		return ResourceChange{
			Action:          "add_to_code",
			URN:             "urn:pulumi:dev::test::pkg:Res::" + name,
			Type:            "pkg:Res",
			Name:            name,
			InputProperties: inputProps,
		}
	}

	dependsOnProp := func(resourceName string) map[string]interface{} {
		return map[string]interface{}{
			"dependsOn": map[string]interface{}{
				"resourceName": resourceName,
				"resourceType": "pkg:Res",
			},
		}
	}

	t.Run("empty slice", func(t *testing.T) {
		result := sortResourcesByDependencies(nil)
		assert.Nil(t, result)
	})

	t.Run("no dependencies - order preserved, level 0", func(t *testing.T) {
		resources := []ResourceChange{
			makeRes("a", map[string]interface{}{"x": 1}),
			makeRes("b", map[string]interface{}{"y": 2}),
		}
		result := sortResourcesByDependencies(resources)
		require.Len(t, result, 2)
		for _, r := range result {
			assert.Equal(t, 0, r.DependencyLevel)
		}
	})

	t.Run("simple chain A depends on B - B comes first at level 0, A at level 1", func(t *testing.T) {
		resources := []ResourceChange{
			makeRes("a", map[string]interface{}{"ref": dependsOnProp("b")}),
			makeRes("b", map[string]interface{}{"x": 1}),
		}
		result := sortResourcesByDependencies(resources)
		require.Len(t, result, 2)
		assert.Equal(t, "b", result[0].Name)
		assert.Equal(t, 0, result[0].DependencyLevel)
		assert.Equal(t, "a", result[1].Name)
		assert.Equal(t, 1, result[1].DependencyLevel)
	})

	t.Run("diamond A->B, A->C, B->D, C->D - D first, then B and C, then A", func(t *testing.T) {
		resources := []ResourceChange{
			makeRes("a", map[string]interface{}{"rb": dependsOnProp("b"), "rc": dependsOnProp("c")}),
			makeRes("b", map[string]interface{}{"rd": dependsOnProp("d")}),
			makeRes("c", map[string]interface{}{"rd": dependsOnProp("d")}),
			makeRes("d", map[string]interface{}{"x": 1}),
		}
		result := sortResourcesByDependencies(resources)
		require.Len(t, result, 4)

		levelByName := make(map[string]int)
		posByName := make(map[string]int)
		for i, r := range result {
			levelByName[r.Name] = r.DependencyLevel
			posByName[r.Name] = i
		}

		assert.Equal(t, 0, levelByName["d"])
		assert.Less(t, posByName["d"], posByName["b"])
		assert.Less(t, posByName["d"], posByName["c"])
		assert.Less(t, posByName["b"], posByName["a"])
		assert.Less(t, posByName["c"], posByName["a"])
		assert.Equal(t, 2, levelByName["a"]) // a is 2 hops from d
	})

	t.Run("external dep (not in batch) - resource treated as level 0", func(t *testing.T) {
		// "a" references "external" which is not in the batch
		resources := []ResourceChange{
			makeRes("a", map[string]interface{}{"ref": dependsOnProp("external")}),
			makeRes("b", map[string]interface{}{"x": 1}),
		}
		result := sortResourcesByDependencies(resources)
		require.Len(t, result, 2)
		for _, r := range result {
			assert.Equal(t, 0, r.DependencyLevel)
		}
	})

	t.Run("array-wrapped dependsOn resolved correctly", func(t *testing.T) {
		// triggers = [{"dependsOn": {...}}] — dependency inside array
		resources := []ResourceChange{
			makeRes("cmd", map[string]interface{}{
				"triggers": []interface{}{dependsOnProp("pass")},
			}),
			makeRes("pass", map[string]interface{}{"length": 16}),
		}
		result := sortResourcesByDependencies(resources)
		require.Len(t, result, 2)
		assert.Equal(t, "pass", result[0].Name)
		assert.Equal(t, 0, result[0].DependencyLevel)
		assert.Equal(t, "cmd", result[1].Name)
		assert.Equal(t, 1, result[1].DependencyLevel)
	})
}

// TestBuildDepMapFromState verifies that buildDepMapFromState correctly resolves
// all property dependencies from the state fixture.
func TestBuildDepMapFromState(t *testing.T) {
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	depMap := buildDepMapFromState(stateLookup)

	// ca-cert.privateKeyPem → ca-key.privateKeyPem
	caCertDeps := depMap["urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::ca-cert"]
	require.NotNil(t, caCertDeps, "ca-cert should have dependency entries")
	pkRef := caCertDeps["privateKeyPem"]
	assert.Equal(t, "ca-key", pkRef.ResourceName)
	assert.Equal(t, "tls:index/privateKey:PrivateKey", pkRef.ResourceType)
	assert.Equal(t, "privateKeyPem", pkRef.OutputProperty)

	// server-cert.caPrivateKeyPem → ca-key.privateKeyPem
	serverDeps := depMap["urn:pulumi:dev::test::tls:index/locallySignedCert:LocallySignedCert::server-cert"]
	require.NotNil(t, serverDeps, "server-cert should have dependency entries")
	caKeyRef := serverDeps["caPrivateKeyPem"]
	assert.Equal(t, "ca-key", caKeyRef.ResourceName)
	assert.Equal(t, "privateKeyPem", caKeyRef.OutputProperty)

	// server-cert.caCertPem → ca-cert.certPem
	caCertRef := serverDeps["caCertPem"]
	assert.Equal(t, "ca-cert", caCertRef.ResourceName)
	assert.Equal(t, "tls:index/selfSignedCert:SelfSignedCert", caCertRef.ResourceType)
	assert.Equal(t, "certPem", caCertRef.OutputProperty)

	// ca-key and server-key have no property dependencies — should not be in dep map
	assert.Nil(t, depMap["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ca-key"])
	assert.Nil(t, depMap["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::server-key"])
}

// TestDepMapRoundTrip verifies save → load produces identical dep map.
func TestDepMapRoundTrip(t *testing.T) {
	original := DependencyMap{
		"urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::ca-cert": {
			"privateKeyPem": {
				ResourceName:   "ca-key",
				ResourceType:   "tls:index/privateKey:PrivateKey",
				OutputProperty: "privateKeyPem",
			},
		},
		"urn:pulumi:dev::test::tls:index/locallySignedCert:LocallySignedCert::server-cert": {
			"caPrivateKeyPem": {
				ResourceName:   "ca-key",
				ResourceType:   "tls:index/privateKey:PrivateKey",
				OutputProperty: "privateKeyPem",
			},
			"caCertPem": {
				ResourceName:   "ca-cert",
				ResourceType:   "tls:index/selfSignedCert:SelfSignedCert",
				OutputProperty: "certPem",
			},
		},
	}

	savedPath, err := saveDepMap(original)
	require.NoError(t, err)
	assert.NotEmpty(t, savedPath)

	loaded, err := loadDepMap(savedPath)
	require.NoError(t, err)

	// Compare each entry
	for urn, props := range original {
		loadedProps, ok := loaded[urn]
		require.True(t, ok, "URN %s missing from loaded dep map", urn)
		for prop, ref := range props {
			loadedRef, ok := loadedProps[prop]
			require.True(t, ok, "property %s missing from loaded dep map for %s", prop, urn)
			assert.Equal(t, ref, loadedRef)
		}
	}
}

// TestDepMapNoSecretValues verifies the dep map file contains no PEM strings or secret values.
func TestDepMapNoSecretValues(t *testing.T) {
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	depMap := buildDepMapFromState(stateLookup)

	path, err := saveDepMap(depMap)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.NotContains(t, content, "BEGIN RSA PRIVATE KEY")
	assert.NotContains(t, content, "BEGIN CERTIFICATE")
	assert.NotContains(t, content, "fake-ca-key")
	assert.NotContains(t, content, "fake-ca-cert")
	assert.NotContains(t, content, "fake-server-key")
	assert.NotContains(t, content, "fake-server-cert")

	// Should contain only metadata
	assert.Contains(t, content, "ca-key")
	assert.Contains(t, content, "privateKeyPem")
	assert.Contains(t, content, "certPem")
}

// TestDepMapSkipsStateExport verifies that --dep-map-file skips state export and still resolves correctly.
func TestDepMapSkipsStateExport(t *testing.T) {
	// First build a dep map from state
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)
	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)
	depMap := buildDepMapFromState(stateLookup)

	tmpDir := t.TempDir()
	depMapPath, err := saveDepMap(depMap)
	require.NoError(t, err)

	// Copy events file
	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	eventsFile := filepath.Join(tmpDir, "events.json")
	require.NoError(t, os.WriteFile(eventsFile, eventsData, 0644))

	// Run with --dep-map-file only (NO --state-file) — state export should not be needed
	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--dep-map-file", depMapPath})

	assert.Equal(t, "changes_needed", summary.Status)

	// Verify dependency resolution still works via dep map
	var caCert *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "ca-cert" {
			caCert = &full.Resources[i]
		}
	}
	require.NotNil(t, caCert)
	pkPem, ok := caCert.InputProperties["privateKeyPem"].(map[string]interface{})
	require.True(t, ok, "privateKeyPem should have dependsOn from dep map")
	dep, ok := pkPem["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-key", dep["resourceName"])
	assert.Equal(t, "privateKeyPem", dep["outputProperty"])
}

// TestDepMapReusedOnSubsequentCalls verifies that the dep map produced on first run
// can be loaded on a subsequent run via --dep-map-file and produces identical results.
func TestDepMapReusedOnSubsequentCalls(t *testing.T) {
	// Build dep map from state fixture (simulates first run)
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)
	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)
	depMap := buildDepMapFromState(stateLookup)

	tmpDir := t.TempDir()
	depMapPath, err := saveDepMap(depMap)
	require.NoError(t, err)

	eventsFile := filepath.Join(tmpDir, "events.json")
	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(eventsFile, eventsData, 0644))

	// "Subsequent run" with --dep-map-file
	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--dep-map-file", depMapPath})

	assert.Equal(t, "changes_needed", summary.Status)
	assert.NotEmpty(t, summary.DepMapFile, "depMapFile should be populated")
	assert.NotEmpty(t, full.DepMapFile, "depMapFile should be in output file")

	// Verify dependency resolution works from dep map
	var caCert *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "ca-cert" {
			caCert = &full.Resources[i]
		}
	}
	require.NotNil(t, caCert)
	pkPem, ok := caCert.InputProperties["privateKeyPem"].(map[string]interface{})
	require.True(t, ok)
	dep, ok := pkPem["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-key", dep["resourceName"])
}

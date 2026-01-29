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

//go:build e2e

// Package e2e contains end-to-end tests for the drift adoption workflow.
package e2e

import (
	"context"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriftAdoptionV2 tests drift adoption using provider-agnostic approach with various scenarios
func TestDriftAdoptionV2(t *testing.T) {
	t.Parallel()

	testCases := GetStandardTestCases()

	for _, tc := range testCases {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			config := DriftTestConfig{
				ExampleDir:    tc.ExampleDir,
				MaxIterations: 10,
			}

			// Step 1: Create and deploy test stack
			testStack := CreateTestStack(t, config.ExampleDir)
			defer testStack.Destroy(t)

			// Verify initial outputs
			tc.VerifyOutputs(t, testStack.Resources)

			// Step 2: Create drift using drifted program (provider-agnostic)
			t.Log("🔧 Step 2: Creating drift using drifted program...")
			err := testStack.CreateDriftWithProgram(t, config.ExampleDir)
			require.NoError(t, err, "Failed to create drift with drifted program")

			// Step 3: Verify drift exists
			t.Log("🔍 Step 3: Verifying drift exists...")
			testStack.Test.Refresh(t)
			preview := testStack.Test.Preview(t)

			changeCount := preview.ChangeSummary[apitype.OpUpdate] + preview.ChangeSummary[apitype.OpCreate]
			assert.True(t, changeCount > 0, "Should have changes detected (drift exists)")
			t.Logf("   ✅ Drift detected: %d change(s)", changeCount)

			// Step 4: Use Claude to fix drift
			t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
			metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
			require.NoError(t, err, "Claude drift adoption failed")
			t.Log("   ✅ Claude completed drift adoption")

			// Log metrics
			t.Log("📊 Drift Adoption Metrics:")
			t.Logf("   Total iterations: %d", metrics.IterationsCount)
			t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
			t.Logf("   Token usage: %d input, %d output, %d total",
				metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

			// Step 5: Verify the code was updated
			t.Log("✅ Step 5: Verifying code was updated...")
			codeContent, err := readFile(testStack.WorkingDir, "index.ts")
			require.NoError(t, err)

			tc.VerifyCode(t, codeContent)
			t.Log("   ✅ Code correctly updated")

			// Step 6: Verify no drift remains
			updates, creates, deletes := testStack.VerifyNoDrift(t)
			assert.Equal(t, 0, updates, "Should have no updates (drift is fixed)")
			assert.Equal(t, 0, creates, "Should have no creates")
			assert.Equal(t, 0, deletes, "Should have no deletes")

			t.Logf("✅✅✅ Drift adoption complete for %s! ✅✅✅", tc.Name)
		})
	}
}

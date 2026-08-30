/*
Copyright 2024 The Kubernetes Incident Investigator Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package investigation

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

// maxNameLength is the maximum length of a Kubernetes resource name (DNS subdomain rule).
const maxNameLength = 63

// GenerateActiveName returns the deterministic active-slot name for a workload incident.
//
// Pattern: <workload-name>-<kind-lowercase>-active
// Example: payment-api-deployment-active, worker-daemonset-active
//
// The result is always ≤ maxNameLength characters.
func GenerateActiveName(workloadName, workloadKind string) string {
	suffix := "-" + strings.ToLower(workloadKind) + "-active"
	return truncateName(workloadName, maxNameLength-len(suffix)) + suffix
}

// GeneratePodActiveName returns the deterministic active-slot name for a Pod-level incident
// (used when workload ownership cannot be resolved).
//
// Pattern: <pod-name>-pod-active
func GeneratePodActiveName(podName string) string {
	const suffix = "-pod-active"
	return truncateName(podName, maxNameLength-len(suffix)) + suffix
}

// GenerateHistoricalName returns a unique historical name for a resolved incident.
//
// Pattern: <workload-name>-<kind-lowercase>-<YYYYMMDD>-<5hex>
//
// <5hex> is the first 5 characters of the hex-encoded FNV-32a hash of:
// "namespace/workloadKind/workloadName/startedAt-unix-seconds"
//
// Hash collisions are practically impossible between successive incidents for the same
// workload because the minimum stability period ensures startedAt values are separated
// by at least StabilityPeriod (default 5 minutes).
//
// The result is always ≤ maxNameLength characters.
func GenerateHistoricalName(workloadName, workloadKind, namespace string, startedAt time.Time) string {
	date := startedAt.UTC().Format("20060102")
	hash := fiveHexHash(namespace, workloadKind, workloadName, startedAt)
	suffix := fmt.Sprintf("-%s-%s-%s", strings.ToLower(workloadKind), date, hash)
	return truncateName(workloadName, maxNameLength-len(suffix)) + suffix
}

// fiveHexHash computes the first 5 hex characters of an FNV-32a hash of the key fields.
func fiveHexHash(namespace, workloadKind, workloadName string, startedAt time.Time) string {
	h := fnv.New32a()
	key := fmt.Sprintf("%s/%s/%s/%d", namespace, workloadKind, workloadName, startedAt.Unix())
	_, _ = h.Write([]byte(key))
	// %05x pads to 5 hex digits; [:5] ensures we never exceed 5 even for large values.
	return fmt.Sprintf("%05x", h.Sum32())[:5]
}

// truncateName shortens s to at most n runes. Returns empty string if n <= 0.
func truncateName(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	return s[:n]
}

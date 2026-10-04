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

package evidence

import (
	"bufio"
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"

	"github.com/k8s-incident-investigator/k8s-incident-investigator/api/v1alpha1"
)

// LogCollector collects Layer 6 bounded container log excerpts.
type LogCollector struct{}

// Collect retrieves bounded log excerpts for the affected containers.
// Log unavailability is recorded inside ContainerLogEvidence.UnavailableReason,
// not as a CollectionError, because log absence is expected for some trigger types.
func (c *LogCollector) Collect(ctx context.Context, input CollectorInput, needs triggerNeeds) ([]v1alpha1.ContainerLogEvidence, []v1alpha1.CollectionError) {
	if input.Pod == nil || input.KubeClient == nil {
		return nil, nil
	}
	if !needs.currentLogs && !needs.previousLogs {
		return nil, nil
	}
	// MaxLogLines == 0 means log collection is disabled — skip entirely, no API call.
	if input.Config.MaxLogLines == 0 {
		return nil, nil
	}

	// Determine which containers to collect logs from.
	var containerNames []string
	if input.Report.Status.Trigger != nil && input.Report.Status.Trigger.ContainerName != "" {
		containerNames = []string{input.Report.Status.Trigger.ContainerName}
	} else {
		for _, c := range input.Pod.Spec.Containers {
			containerNames = append(containerNames, c.Name)
		}
	}

	maxLines := int64(input.Config.MaxLogLines)
	var result []v1alpha1.ContainerLogEvidence

	for _, name := range containerNames {
		// Current logs
		if needs.currentLogs {
			cle := collectContainerLogs(ctx, input, name, false, maxLines)
			result = append(result, cle)
		}
		// Previous logs (for terminated containers)
		if needs.previousLogs {
			cle := collectContainerLogs(ctx, input, name, true, maxLines)
			// Only include previous logs if we got something useful
			if cle.UnavailableReason == "" || len(cle.Lines) > 0 {
				result = append(result, cle)
			}
		}
	}
	return result, nil
}

func collectContainerLogs(ctx context.Context, input CollectorInput, containerName string, previous bool, maxLines int64) v1alpha1.ContainerLogEvidence {
	cle := v1alpha1.ContainerLogEvidence{
		ContainerName: containerName,
		IsPrevious:    previous,
	}

	opts := &corev1.PodLogOptions{
		Container: containerName,
		Previous:  previous,
		TailLines: &maxLines,
	}

	req := input.KubeClient.CoreV1().Pods(input.Pod.Namespace).GetLogs(input.Pod.Name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		cle.UnavailableReason = err.Error()
		return cle
	}
	defer stream.Close()

	// Enforce byte limit via LimitedReader
	limitedReader := io.LimitReader(stream, int64(input.Config.MaxLogBytes))
	scanner := bufio.NewScanner(limitedReader)

	var lines []string
	bytesRead := 0
	for scanner.Scan() {
		line := scanner.Text()
		bytesRead += len(line) + 1 // +1 for newline
		lines = append(lines, line)
		if len(lines) >= int(maxLines) || bytesRead >= input.Config.MaxLogBytes {
			cle.Truncated = true
			break
		}
	}

	cle.Lines = lines

	// If the reader was exhausted by the byte limit, we may have been truncated
	// even if we didn't hit the line limit. Check if there's more data.
	if !cle.Truncated {
		// Try to read one more byte to detect truncation
		buf := make([]byte, 1)
		n, _ := stream.Read(buf)
		if n > 0 {
			cle.Truncated = true
		}
	}

	return cle
}

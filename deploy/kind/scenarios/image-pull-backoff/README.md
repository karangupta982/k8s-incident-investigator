# Scenario: ImagePullBackOff

## What it does
References an image that does not exist in any registry. Kubernetes immediately
fails to pull it and enters ImagePullBackOff.

## Expected result
- Phase: Diagnosed
- Primary finding: `ImagePullFailure` (High confidence)
- Recommendation references the image name

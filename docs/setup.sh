!/usr/bin/env bash

set -euo pipefail

PROJECT="."

mkdir -p "$PROJECT"
cd "$PROJECT"

# --------------------------------------------------
# Kiro
# --------------------------------------------------

mkdir -p .kiro/steering
mkdir -p .kiro/skills/kubernetes-incident-investigator
mkdir -p .kiro/specs
mkdir -p .kiro/hooks
mkdir -p .kiro/settings

touch \
  .kiro/steering/product.md \
  .kiro/steering/architecture.md \
  .kiro/steering/tech.md \
  .kiro/steering/structure.md \
  .kiro/skills/kubernetes-incident-investigator/SKILL.md \
  .kiro/skills/kubernetes-incident-investigator/architecture.md \
  .kiro/skills/kubernetes-incident-investigator/investigation-patterns.md \
  .kiro/hooks/hooks.json \
  .kiro/settings/mcp.json

# --------------------------------------------------
# Application structure
# --------------------------------------------------

mkdir -p api/v1alpha1

mkdir -p cmd

mkdir -p internal/controller
mkdir -p internal/investigation
mkdir -p internal/evidence
mkdir -p internal/diagnosis/rules
mkdir -p internal/reporting
mkdir -p internal/config

# --------------------------------------------------
# Kubernetes configuration
# --------------------------------------------------

mkdir -p config/crd
mkdir -p config/rbac
mkdir -p config/manager
mkdir -p config/samples

# --------------------------------------------------
# Tests
# --------------------------------------------------

mkdir -p test/unit
mkdir -p test/integration

# --------------------------------------------------
# Documentation
# --------------------------------------------------

mkdir -p docs/architecture
mkdir -p docs/design-decisions
mkdir -p docs/demo

# --------------------------------------------------
# Local deployment
# --------------------------------------------------

mkdir -p deploy/kind

# --------------------------------------------------
# Root files
# --------------------------------------------------

touch \
  README.md \
  .gitignore \
  .kiroignore \
  Dockerfile \
  Makefile

echo
echo "Initial project structure created:"
echo
find . -type f -o -type d | sort
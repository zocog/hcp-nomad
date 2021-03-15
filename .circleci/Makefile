# Set SHELL to 'strict mode' without using .SHELLFLAGS for max compatibility.
# See https://fieldnotes.tech/how-to-shell-for-compatible-makefiles/
SHELL := /usr/bin/env bash -euo pipefail -c

CIRCLECI_CLI := circleci --skip-update-check
ifeq ($(CIRCLECI),true)
	CIRCLECI_CLI := circleci-local-cli --skip-update-check
endif

# Set up some documentation/help message variables.
# We do not attempt to install the CircleCI CLI from this Makefile.
CCI_INSTALL_LINK := https://circleci.com/docs/2.0/local-cli/\#installation
CCI_INSTALL_MSG := Please install CircleCI CLI. See $(CCI_INSTALL_LINK)
CCI_VERSION := $(shell $(CIRCLECI_CLI) version 2> /dev/null)
ifeq ($(CCI_VERSION),)
# Attempting to use the CLI fails with installation instructions.
CIRCLECI_CLI := echo '$(CCI_INSTALL_MSG)'; exit 1; \#
endif

default: help

help:
	@echo "Usage:"
	@echo "  make config.yml: generate that config.yml"
	@echo "  make ci-verify: validate that config.yml for correctness"
	@echo
	@echo "Diagnostics:"
	@[ -z "$(CCI_VERSION)" ] || echo "  circleci-cli version $(CCI_VERSION)"
	@[ -n "$(CCI_VERSION)" ] || echo "  $(CCI_INSTALL_MSG)"

.PHONY: ci-verify
ci-verify:
	@$(CIRCLECI_CLI) config validate config.yml

MKFILE_PATH := $(lastword $(filter %Makefile,$(MAKEFILE_LIST)))
CURRENT_DIR := $(dir $(realpath $(MKFILE_PATH)))
CURRENT_DIR := $(CURRENT_DIR:/=)

ifneq ($(wildcard $(CURRENT_DIR)/*.mk),)
include $(CURRENT_DIR)/*.mk
endif

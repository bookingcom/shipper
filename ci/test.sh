#!/bin/bash -ex

TEST_FLAGS="-v" make verify-codegen lint test

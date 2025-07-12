# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Comprehensive CI/CD workflows with GitHub Actions
  - Automated testing on Go 1.23 and 1.24
  - Linting with golangci-lint
  - Example application testing
  - Multi-platform binary releases
  - Weekly dependency updates
- Enhanced error handling throughout the codebase
- Code coverage reporting via Codecov

### Changed
- **BREAKING**: Upgraded to Modular v1.3.9
  - Added support for new `IsVerboseConfig()` and `SetVerboseConfig()` methods
  - Added support for new `SetLogger()` method
  - Updated all mock implementations to match new interface requirements
- Improved error handling for service registration and I/O operations
- Enhanced logging consistency across modules

### Fixed
- Fixed critical error checking issues identified by linters
- Resolved HTTP response writing error handling
- Fixed service registration error handling in engine
- Improved test reliability and isolation

### Security
- Enhanced dependency management with automated updates
- Improved error handling to prevent potential runtime issues

## [Previous Versions]

Previous version history was not maintained in changelog format.
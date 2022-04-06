# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]
### Fixed
- Metrics endpoint returning 404
- Frontend: Split homepage into "Recent tasks" page and "History tasks page"
- Frontend: Add routing to the application
- Frontend: Refactor UI components to be more maintainable

## [0.0.2] - 2022-04-02
### Added
- An endpoint to get a list of existing app names
- Health Check endpoint
- Application filter in Web UI
- Custom timeframe selection with DatePicker in Web UI
- Metrics endpoint (currently with a single failed_deployment metric)
### Fixed
- Make client print output during execution

## [0.0.1] - 2022-03-28
### Added
- Initial version

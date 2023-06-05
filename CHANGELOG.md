# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.1] - 2023-06-05

### Fixed
- Task details in the UI while using postgres state

## [0.4.0] - 2023-05-18

### Added

- Support for in cluster docker proxy registry
- Dropdown for pagination which persists over page reload and browser reopen
- Reworked server core to support mocked unit tests

### Changed

- Logs are now sent in JSON format
- Table style minor improvements
- Rework for item errors in the table. Replace popup with slide-in

### Fixed
- Fix for date-time invalid format breaking the page

## [0.3.2] - 2023-01-24

### Changed

- ARGO_USER and ARGO_PASSWORD was replaced with ARGO_TOKEN

## [0.3.1] - 2022-11-24

### Fixed

- Token expiration issue

## [0.3.0] - 2022-11-14

### Added

- Extensive error handling to communication with ArgoCD
- Display of additional information for predictable errors (app not found, sync failed, health degraded, etc)
- More control over API calls and App status checks
- Debug information regarding ArgoCD API authentication status

## [0.2.0] - 2022-10-18

### Added

- An additional metric is exposed to indicate whether argo-watcher is successfully connected to ArgoCD
- Front-End prettier support. We can run full project format with `npm run format` command.

### Changed

- ArgoCD connectivity is no longer considered a requirement to keep argo watcher alive
- Front-End API call error handling. Now we monitor each separate API call and display error/success messages for each call type.

## [0.1.4] - 2022-08-17

### Changed

- Release automation (added automatic helm chart update)

## [0.1.3] - 2022-08-09

### Added

- Swagger UI /swagger/index.html
- date and time now is showing in UTC
- changed primary color for the theme
- added pagination to the pages
- other minor improvements

## [0.1.2] - 2022-08-02

### Changed

- Tasks with a status "app not found" are removed after 1h as they provide no value in history
- Tasks with a status "in progress" are marked as "aborted" after 1h since that's an indicator of argo-watcher crash
- Show full time instead of relative on history page

## [0.1.1] - 2022-07-27

### Fixed

- url to github project

## [0.1.0] - 2022-07-19

### Added

- added date range picker to history page, now it is possible to choose range instead of single date

### Changed

- both "recent tasks" page and "history" page now save their filters to URL to support links sharing

### Fixed

- e is null issue
- GetAppList for InMemoryState

## [0.0.12] - 2022-06-17

### Changed

- Improved waitForRollout logic
- strings.TrimSuffix for client to handle cases when url is provided with trailing slash

## [0.0.11] - 2022-06-15

### Fixed

- waitForRollout for apps with multiple images
- Validate application status

## [0.0.10] - 2022-06-13

### Changed

- Version display in Navigation

## [0.0.9] - 2022-06-09

### Changed

- Watcher was rewritten in Go

## [0.0.8] - 2022-06-03

### Fixed

- Set correct exit code for client

## [0.0.7] - 2022-05-31

### Changed

- Client was rewritten to golang

## [0.0.6] - 2022-05-25

### Added

- Add version endpoint
- Health Check endpoint now detects database problem

### Changed

- Minor changes in state

### Fixed

- set default value for db_port
- UI bug for Task Duration under 1 minute

## [0.0.5] - 2022-05-18

### Added

- Frontend: status badges

### Changed

- Migrated DBState to SQLAlchemy
- Add support for PostgreSQL port configuration
- Frontend: fqdns are hyperlinks now
- Frontend: minor improvements of the layout

### Fixed

- InMemoryState time_range_to filter

## [0.0.4] - 2022-05-02

### Added

- Support for providing end timestamp to be able to configure more reasonable timeframe

### Changed

- Exit 1 if received 401 or 403 from ArgoCD
- Falling back to in-memory state if invalid value was provided in STATE_TYPE
- Settings were renamed to Config and environment variable processing was changed

### Fixed

- 404 on /history page reload/direct access

## [0.0.3] - 2022-04-06

### Added

- Frontend: Split homepage into "Recent tasks" page and "History tasks" page
- Frontend: Add routing to the application

### Changed

- Make ssl verification optional
- Logging approach

### Fixed

- Metrics endpoint returning 404
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

# Goal Description
Add a new two-tab interface to the `net` (Network Diagnostics) view in DevDesk:
- **Tab 1: Diagnostics** (The current viewport content: Ping, DNS, Traceroute, etc.)
- **Tab 2: Ports** (Real-time tracking of active ports using the `ss` command via the configured network tool Docker image)

## Proposed Changes

### UI & Tab Navigation
#### [MODIFY] internal/ui/netdiag/model.go
- Add an `ActiveTab` integer state (`0` for Diagnostics, `1` for Ports).
- Update the `View()` function to render a Tab header (`[ Diagnostics ]  [ Ports ]`) immediately below the viewport header (above the current form content).
- Re-bind `tab` and `shift+tab` to switch between the "Diagnostics" and "Ports" tabs.
- Change the navigation inside the "Diagnostics" tab (Target, Port, tests) to use exclusively the `Up` and `Down` arrow keys, adhering to the standard form navigation UX.
- Ensure that if `ActiveTab == 1` (Ports), key events and `Update()` calls are routed to the new `PortsModel`.

### Ports Model
#### [NEW] internal/ui/netdiag/ports_model.go
- Create a Bubble Tea `PortsModel` with state:
  - List of parsed process ports.
  - Bubble Tea table to render the data.
  - Filters state: `tcp` (t), `udp` (u), `listen` (l), `establish` (e), `all` (a).
  - Paused state triggered by `space`.
  - Search input triggered by `/`.
- Implement `Tick` function sending a command every 2-3 seconds to refresh data using Docker.
- Implement keybindings for filtering, searching, freezing, and `ctrl+k` (which triggers a Docker kill command on the selected PID).

### Docker Integration
#### [MODIFY] internal/docker/network.go
- Add a new struct `PortInfo` representing a parsed row: `PID`, `Process`, `State`, `LocalAddr`, `PeerAddr`.
- Add a function `RunSS(image string) ([]PortInfo, error)`:
  - Runs `docker run --rm --net=host --pid=host <image> ss -tupan`.
  - Parses the raw text output into the `[]PortInfo` struct.
- Add a function `KillProcess(image string, pid string) error`:
  - Runs `docker run --rm --pid=host --privileged <image> kill -9 <pid>`.

> [!CAUTION]
> Using `ss` and reading host ports requires the Docker container to be run with `--net=host` and `--pid=host`. Furthermore, to allow killing a process via `ctrl+k`, the container must run as `--privileged` to send the `SIGKILL` signal to processes on the host. We will add these capabilities to the `network_tool` container launch options when running the `ss` command. 

## Verification Plan
### Automated Tests
- We will add a unit test in `internal/docker/network_test.go` (if it exists) or create one to verify that the `ss -tupan` raw output is correctly parsed into the `[]PortInfo` structure, even when some fields (like PID or Process name) are missing.

### Manual Verification
1. Launch devdesk and navigate to the `net` view.
2. Verify that `[ Diagnostics ]` and `[ Ports ]` tabs are visible below the title.
3. Use `tab` / `shift+tab` to switch to the Ports tab.
4. Verify the form inside Diagnostics is traversable only with `Up` / `Down` arrows.
5. Verify the `Ports` table populates with real-time data from the host.
6. Setup a dummy process on the host (e.g., `nc -l -p 9999`) and locate it.
7. Test filters by pressing `t`, `u`, `l`, `e` and type `/9999` to filter by port.
8. Select the dummy `nc` process and press `ctrl+k`.
9. Verify that the process is terminated on the host and disappears from the DevDesk list.

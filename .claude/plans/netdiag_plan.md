# Network Diagnostics (Netdiag) Integration

This plan implements a new `:netdiag` view in DevDesk to troubleshoot network issues using the network diagnostic image configured in `config.Docker.NetworkMultitoolImage`. It allows the user to configure the target and select specific diagnostic checks via checkboxes. The checks are executed in parallel and the results are displayed in a summary table with Nerd Font icons.

## Proposed Changes

### Model and Command Parser
Update the navigation map to support the new command.
#### [MODIFY] internal/command/parser.go
- Add `ViewNetdiag` constant.
- Add `:netdiag` and `:net` to the view map and aliases.

#### [MODIFY] internal/app/app.go
- Register `netdiag.New(a.config)` in the `views` map.

---

### Docker Execution Logic
Create a service to orchestrate the Docker calls.
#### [NEW] internal/docker/netdiag.go
Create functions to run specific commands in the container. The Docker image string will be passed from the configuration (`cfg.Docker.NetworkMultitoolImage`). All commands will run with `--network host`.

- **`RunPing(image, target)`**: executes `ping -c 3 <target>`
- **`RunDNS(image, target)`**: executes `nslookup <target>`
- **`RunTraceroute(image, target)`**: executes `traceroute -m 15 -w 1 <target>`
- **`RunNetcat(image, target, port)`**: executes `nc -zv -w 2 <target> <port>`
- **`RunCurl(image, target, port)`**: executes `curl -sS -I --max-time 3 <scheme>://<target>:<port>`
- **`RunSSLCert(image, target, port)`**: executes `echo | openssl s_client -showcerts -servername <target> -connect <target>:<port> 2>/dev/null | openssl x509 -inform pem -noout -text | grep "Issuer\|Subject\|Not After"`

Each function returns the parsed success/failure status and message.

---

### Network Diagnostics UI (Netdiag View)
Create the new Bubble Tea view. The flow will be: `Target Input` -> `Test Selection (Checkboxes)` -> `Parallel Execution` -> `Results Table`.

#### [NEW] internal/ui/netdiag/model.go
- Define the state machine (`StateInputTarget`, `StateInputPort`, `StateSelectTests`, `StateRunning`, `StateResults`).
- Add fields for the inputs: `target` (textinput), `port` (textinput).
- Add a checkbox list model (using `bubbles/list` or custom toggle list) for the test options:
  - `[x] Ping`
  - `[x] DNS Resolution`
  - `[x] Traceroute`
  - `[x] Netcat`
  - `[x] HTTP/HTTPS (Curl)`
  - `[x] SSL Certificate Check`

#### [NEW] internal/ui/netdiag/update.go
- Form Navigation: handle `Enter` to move between Target, Port, and Test Selection. Space to toggle checkboxes.
- Execution: On start, construct an array of `tea.Cmd` corresponding only to the selected tests.
- Dispatch them using `tea.Batch(cmds...)` to run them concurrently.
- As results come back via messages (e.g., `PingResultMsg`, `CurlResultMsg`), update the internal results map.
- Once all selected tests have returned a result, transition smoothly to the final view or allow real-time UI updates during execution.

#### [NEW] internal/ui/netdiag/view.go
- **Form Views**: Render the text inputs and the interactive checkbox list.
- **Progress View**: Render spinning icons next to each running test in real-time.
- **Results View**: Render a final summary table using `lipgloss.Table` and NerdFonts (e.g., `` for success, `` for failure, `` for loading) containing the test, output snippet, and status.

---

## Verification Plan

### Automated Tests
- Unit tests for the parser in `internal/command/parser_test.go` to ensure `:netdiag` is correctly mapped.

### Manual Verification
1. Launch DevDesk and type `:netdiag`.
2. Enter `google.com` as target, and `443` as port.
3. Select specific tests using Space in the checkbox menu (e.g., Ping, Curl, SSL).
4. Press Enter to start.
5. Verify that the UI updates in parallel. You should see Spinners on multiple lines simultaneously.
6. Verify the final table shows `` (Success) with certificate details visible for the SSL check.

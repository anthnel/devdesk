package containers

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gitlab.com/anthnell/devsecops/devdesk/internal/docker"
)

const refreshInterval = 2 * time.Second

// tickCmd returns a tick command for periodic refresh
func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return RefreshTickMsg(t)
	})
}

// fetchContainers fetches the container list
func fetchContainers(all bool) tea.Cmd {
	return func() tea.Msg {
		containers, err := docker.ListContainers(all)
		return ContainersListMsg{Containers: containers, Err: err}
	}
}

// fetchMetrics fetches container metrics
func fetchMetrics() tea.Cmd {
	return func() tea.Msg {
		metrics, err := docker.GetContainerMetrics()
		return ContainerMetricsMsg{Metrics: metrics, Err: err}
	}
}

// stopContainer stops a container
func stopContainer(id, name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.StopContainer(id)
		return ContainerActionMsg{Action: "stop", ID: name, Err: err}
	}
}

// restartContainer restarts a container
func restartContainer(id, name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.RestartContainer(id)
		return ContainerActionMsg{Action: "restart", ID: name, Err: err}
	}
}

// removeContainer removes a container
func removeContainer(id, name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.RemoveContainer(id, false)
		return ContainerActionMsg{Action: "remove", ID: name, Err: err}
	}
}

// pauseContainer pauses a running container
func pauseContainer(id, name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.PauseContainer(id)
		return ContainerActionMsg{Action: "pause", ID: name, Err: err}
	}
}

// unpauseContainer resumes a paused container
func unpauseContainer(id, name string) tea.Cmd {
	return func() tea.Msg {
		err := docker.UnpauseContainer(id)
		return ContainerActionMsg{Action: "unpause", ID: name, Err: err}
	}
}

// pruneContainers removes all stopped containers
func pruneContainers() tea.Cmd {
	return func() tea.Msg {
		output, err := docker.PruneContainers()
		return ContainerPruneMsg{Output: output, Err: err}
	}
}

// fetchContainerLogs fetches the last 500 lines of logs for a container.
// Pass timestamps=true to prefix each line with its RFC3339Nano timestamp.
func fetchContainerLogs(id string, timestamps bool) tea.Cmd {
	return func() tea.Msg {
		content, err := docker.GetContainerLogs(id, 500, timestamps)
		return ContainerLogsLoadedMsg{Content: content, Err: err}
	}
}

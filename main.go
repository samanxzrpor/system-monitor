package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

type Config struct {
	RefreshInterval time.Duration
	CPULoadAlert    float64
}

var (
	config = Config{
		RefreshInterval: 1 * time.Second,
		CPULoadAlert:    80.0,
	}
)

func initializeUI() (*widgets.Gauge, *widgets.SparklineGroup, *widgets.SparklineGroup, *widgets.Table) {
	if err := ui.Init(); err != nil {
		panic(fmt.Sprintf("failed to initialize termui: %v", err))
	}

	// CPU Gauge
	cpuGauge := widgets.NewGauge()
	cpuGauge.Title = " CPU Usage "
	cpuGauge.BarColor = ui.ColorGreen
	cpuGauge.BorderStyle.Fg = ui.ColorWhite

	// Memory Sparkline
	memSpark := widgets.NewSparkline()
	memSpark.Data = []float64{}
	memSpark.LineColor = ui.ColorBlue

	// Network Sparkline
	netSpark := widgets.NewSparkline()
	netSpark.Data = []float64{}
	netSpark.LineColor = ui.ColorYellow

	// Process Table
	procTable := widgets.NewTable()
	procTable.Title = " Top Processes "
	procTable.Rows = [][]string{{"PID", "Name", "CPU%"}}
	procTable.TextStyle = ui.NewStyle(ui.ColorWhite)
	procTable.BorderStyle.Fg = ui.ColorCyan

	return cpuGauge, widgets.NewSparklineGroup(memSpark),
		widgets.NewSparklineGroup(netSpark), procTable
}

func collectCPU(interval time.Duration) <-chan float64 {
	ch := make(chan float64)
	go func() {
		for {
			percent, _ := cpu.Percent(interval, false)
			ch <- percent[0]
		}
	}()
	return ch
}

func collectMemory(interval time.Duration) <-chan float64 {
	ch := make(chan float64)
	go func() {
		for {
			mem, _ := mem.VirtualMemory()
			ch <- mem.UsedPercent
			time.Sleep(interval)
		}
	}()
	return ch
}

func collectNetwork(interval time.Duration) <-chan float64 {
	ch := make(chan float64)
	go func() {
		var prevSent, prevRecv uint64
		for {
			stats, _ := net.IOCounters(false)
			if len(stats) > 0 {
				currentSent := stats[0].BytesSent
				currentRecv := stats[0].BytesRecv
				ch <- float64(currentSent-prevSent) / 1024
				ch <- float64(currentRecv-prevRecv) / 1024
				prevSent, prevRecv = currentSent, currentRecv
			}
			time.Sleep(interval)
		}
	}()
	return ch
}

func main() {
	cpuGauge, memSparkGroup, netSparkGroup, procTable := initializeUI()
	defer ui.Close()

	termWidth, termHeight := ui.TerminalDimensions()
	grid := ui.NewGrid()
	grid.SetRect(0, 0, termWidth, termHeight)

	// Layout Setup
	grid.Set(
		ui.NewRow(0.3,
			ui.NewCol(0.5, cpuGauge),
			ui.NewCol(0.5, memSparkGroup),
		),
		ui.NewRow(0.7,
			ui.NewCol(0.7, netSparkGroup),
			ui.NewCol(0.3, procTable),
		),
	)

	cpuChan := collectCPU(config.RefreshInterval)
	memChan := collectMemory(config.RefreshInterval)
	netChan := collectNetwork(config.RefreshInterval)

	// Handle terminal resize
	uiEvents := ui.PollEvents()
	ticker := time.NewTicker(config.RefreshInterval).C

	// Signal handling for clean exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Main loop
	for {
		select {
		case <-sigChan:
			return
		case e := <-uiEvents:
			if e.Type == ui.KeyboardEvent && e.ID == "q" {
				return
			}
			if e.ID == "<Resize>" {
				payload := e.Payload.(ui.Resize)
				grid.SetRect(0, 0, payload.Width, payload.Height)
				ui.Clear()
				ui.Render(grid)
			}
		case <-ticker:
			// Update CPU
			select {
			case cpuPercent := <-cpuChan:
				cpuGauge.Percent = int(cpuPercent)
				if cpuPercent > config.CPULoadAlert {
					cpuGauge.BarColor = ui.ColorRed
				} else {
					cpuGauge.BarColor = ui.ColorGreen
				}
			default:
			}

			// Update Memory
			select {
			case memPercent := <-memChan:
				memSpark := memSparkGroup.Sparklines[0]
				memSpark.Data = append(memSpark.Data, memPercent)
				if len(memSpark.Data) > 50 {
					memSpark.Data = memSpark.Data[1:]
				}
			default:
			}

			// Update Network
			select {
			case netStat := <-netChan:
				netSpark := netSparkGroup.Sparklines[0]
				netSpark.Data = append(netSpark.Data, netStat)
				if len(netSpark.Data) > 50 {
					netSpark.Data = netSpark.Data[1:]
				}
			default:
			}

			ui.Render(grid)
		}
	}
}

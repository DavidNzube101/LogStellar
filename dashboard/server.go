package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	"logstellar/database"
	"logstellar/gpu"
)

type Server struct {
	port    int
	alerts  []Alert
	stats   Stats
	mu      sync.RWMutex
	rpcMode string // "devnet" or "mainnet"
	rpcMu   sync.RWMutex
	db      *database.Client
	scanner *gpu.Scanner
}

type Alert struct {
	ID        int
	Message   string
	Result    gpu.ScanResult
	Timestamp time.Time
}

type Stats struct {
	TotalLogs      int
	TotalAlerts    int
	ProcessingTime time.Duration
	LastUpdate     time.Time
}

func NewServer(port int, db *database.Client, scanner *gpu.Scanner) *Server {
	return &Server{
		port:    port,
		alerts:  make([]Alert, 0),
		stats:   Stats{},
		rpcMode: "devnet", // Default
		db:      db,
		scanner: scanner,
	}
}

func (s *Server) Start() {
	http.HandleFunc("/", s.handleHome)
	http.HandleFunc("/api/alerts", s.handleAlerts)
	http.HandleFunc("/api/logs", s.handleLogs)
	http.HandleFunc("/api/stats", s.handleStats)
	http.HandleFunc("/api/gpu-stats", s.handleGPUStats)
	http.HandleFunc("/api/rpc-mode", s.handleRPCMode)
	http.HandleFunc("/api/switch-rpc", s.handleSwitchRPC)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Dashboard server starting on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start dashboard: %v", err)
	}
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		json.NewEncoder(w).Encode([]database.LogEntry{})
		return
	}
	logs, err := s.db.GetRecentLogs(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logs)
}

func (s *Server) handleGPUStats(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "offline"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.scanner.GPUStats())
}

func (s *Server) AddAlert(message string, result gpu.ScanResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	alert := Alert{
		ID:        int(time.Now().UnixNano()),
		Message:   message,
		Result:    result,
		Timestamp: time.Now(),
	}

	s.alerts = append([]Alert{alert}, s.alerts...)
	if len(s.alerts) > 100 {
		s.alerts = s.alerts[:100]
	}
}

func (s *Server) UpdateStats(logsProcessed int, duration time.Duration, alertCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stats.TotalLogs += logsProcessed
	s.stats.TotalAlerts += alertCount
	s.stats.ProcessingTime = duration
	s.stats.LastUpdate = time.Now()
}

func (s *Server) GetRPCMode() string {
	s.rpcMu.RLock()
	defer s.rpcMu.RUnlock()
	return s.rpcMode
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
    <meta charset="UTF-8">
    <title>LOGSTELLAR // TERMINAL</title>
    <script src="https://unpkg.com/lucide@latest"></script>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;600;800&family=JetBrains+Mono&display=swap" rel="stylesheet">
    <style>
        :root[data-theme="dark"] {
            --bg: #0a0a0a;
            --surface: #141414;
            --border: #262626;
            --text-main: #e5e5e5;
            --text-dim: #737373;
            --accent: #00ff41;
            --accent-dim: rgba(0, 255, 65, 0.05);
            --header-bg: #000000;
        }
        :root[data-theme="light"] {
            --bg: #ffffff;
            --surface: #f5f5f5;
            --border: #e5e5e5;
            --text-main: #171717;
            --text-dim: #737373;
            --accent: #2563eb;
            --accent-dim: #f0f7ff;
            --header-bg: #f8fafc;
        }

        * { margin: 0; padding: 0; box-sizing: border-box; border-radius: 0 !important; }
        
        body {
            font-family: 'Inter', sans-serif;
            background-color: var(--bg);
            color: var(--text-main);
            overflow: hidden;
            height: 100vh;
            display: flex;
            flex-direction: column;
        }

        .top-bar {
            height: 60px;
            background: var(--header-bg);
            border-bottom: 2px solid var(--border);
            display: flex;
            align-items: center;
            padding: 0 24px;
            justify-content: space-between;
        }

        .logo {
            font-weight: 800;
            letter-spacing: -1px;
            font-size: 1rem;
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .top-bar-controls {
            display: flex;
            align-items: center;
            gap: 12px;
        }

        .rpc-switcher {
            display: flex;
            border: 1px solid var(--border);
            overflow: hidden;
        }

        .rpc-btn {
            padding: 6px 16px;
            background: transparent;
            border: none;
            color: var(--text-dim);
            cursor: pointer;
            font-size: 11px;
            font-weight: 600;
            font-family: 'JetBrains Mono';
            transition: all 0.1s;
        }

        .rpc-btn.active {
            background: var(--accent);
            color: #000;
        }

        .rpc-btn:hover:not(.active) {
            background: var(--accent-dim);
            color: var(--text-main);
        }

        .stats-strip {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            border-bottom: 1px solid var(--border);
        }

        .stat-item {
            padding: 16px 24px;
            border-right: 1px solid var(--border);
        }

        .stat-label {
            font-size: 10px;
            text-transform: uppercase;
            letter-spacing: 1.5px;
            color: var(--text-dim);
            margin-bottom: 4px;
            display: flex;
            align-items: center;
            gap: 6px;
        }

        .stat-value {
            font-family: 'JetBrains Mono';
            font-size: 1.4rem;
            font-weight: 600;
        }

        .main-view {
            display: grid;
            grid-template-columns: 1fr 1fr;
            flex: 1;
            overflow: hidden;
            gap: 0;
        }

        .alerts-panel {
            border-right: 1px solid var(--border);
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        .alerts-list {
            flex: 1;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
        }

        .analytics-panel {
            display: flex;
            flex-direction: column;
            overflow: hidden;
            background: var(--surface);
        }

        .chart-container {
            padding: 20px;
            border-bottom: 1px solid var(--border);
            position: relative;
            display: flex;
            flex-direction: column;
        }

        .chart-wrapper {
            flex: 1;
            position: relative;
            min-height: 0;
        }

        .chart-container:last-child {
            border-bottom: none;
        }

        .chart-header {
            font-family: 'JetBrains Mono';
            font-size: 11px;
            text-transform: uppercase;
            letter-spacing: 1.5px;
            color: var(--text-dim);
            margin-bottom: 16px;
            display: flex;
            align-items: center;
            gap: 8px;
        }

        canvas {
            max-height: 100%;
        }

        .search-bar {
            position: sticky;
            top: 0;
            background: var(--bg);
            padding: 16px 20px;
            border-bottom: 1px solid var(--border);
            z-index: 10;
        }

        .search-input {
            width: 100%;
            padding: 12px 16px;
            background: var(--surface);
            border: 1px solid var(--border);
            color: var(--text-main);
            font-family: 'JetBrains Mono';
            font-size: 13px;
        }

        .search-input:focus {
            outline: none;
            border-color: var(--accent);
        }

        .search-input::placeholder {
            color: var(--text-dim);
        }

        .alerts-content {
            flex: 1;
            padding: 20px;
        }

        .alert-card {
            background: var(--surface);
            border: 1px solid var(--border);
            padding: 16px;
            margin-bottom: 8px;
            cursor: pointer;
            transition: all 0.1s ease;
            position: relative;
        }

        .alert-card:hover {
            border-color: var(--accent);
            background: var(--accent-dim);
        }

        .alert-header {
            display: flex;
            justify-content: space-between;
            margin-bottom: 8px;
        }

        .pattern-tag {
            font-family: 'JetBrains Mono';
            font-size: 11px;
            background: var(--bg);
            padding: 2px 8px;
            border: 1px solid var(--border);
            color: var(--accent);
        }

        .drawer {
            width: 500px;
            background: var(--surface);
            border-left: 1px solid var(--border);
            transform: translateX(100%);
            transition: transform 0.2s ease-out;
            position: fixed;
            top: 60px; right: 0; bottom: 0;
            padding: 24px;
            z-index: 100;
            box-shadow: -10px 0 30px rgba(0,0,0,0.2);
        }

        .drawer.open { transform: translateX(0); }

        .json-block {
            font-family: 'JetBrains Mono';
            font-size: 12px;
            background: #000;
            color: #00ff41;
            padding: 16px;
            overflow-x: auto;
            border: 1px solid #333;
            margin-top: 12px;
        }

        .theme-toggle {
            cursor: pointer;
            padding: 6px 12px;
            border: 1px solid var(--border);
            font-size: 11px;
            background: transparent;
            color: var(--text-main);
            display: flex;
            align-items: center;
            gap: 8px;
        }

        .theme-toggle:hover { background: var(--text-main); color: var(--bg); }
        
        .icon-sm { width: 14px; height: 14px; }
        .icon-xs { width: 12px; height: 12px; stroke-width: 3px; }

        .rpc-indicator {
            font-size: 10px;
            color: var(--text-dim);
            font-family: 'JetBrains Mono';
            letter-spacing: 1px;
        }
    </style>
</head>
<body>
    <header class="top-bar">
        <div class="logo">
            <i data-lucide="cpu" style="color:var(--accent)"></i>
            <span>LOGSTELLAR // <span style="color:var(--text-dim)">SYSTEM_v1.0</span></span>
        </div>
        <div class="top-bar-controls">
            <div class="rpc-indicator" id="rpc-indicator">RPC: DEVNET</div>
            <div class="rpc-switcher">
                <button class="rpc-btn active" id="btn-devnet" onclick="switchRPC('devnet')">DEVNET</button>
                <button class="rpc-btn" id="btn-mainnet" onclick="switchRPC('mainnet')">MAINNET</button>
            </div>
            <button class="theme-toggle" onclick="toggleTheme()">
                <i data-lucide="sun-moon" class="icon-sm"></i>
                SWITCH_MODE
            </button>
        </div>
    </header>

    <div class="stats-strip">
        <div class="stat-item">
            <div class="stat-label"><i data-lucide="database" class="icon-xs"></i> Logs_Ingested</div>
            <div class="stat-value" id="total-logs">0</div>
        </div>
        <div class="stat-item">
            <div class="stat-label"><i data-lucide="zap" class="icon-xs"></i> Signals_Captured</div>
            <div class="stat-value" id="total-alerts" style="color: var(--accent)">0</div>
        </div>
        <div class="stat-item">
            <div class="stat-label"><i data-lucide="activity" class="icon-xs"></i> Latency_MS</div>
            <div class="stat-value" id="processing-time">0.00</div>
        </div>
        <div class="stat-item" style="border-right: none">
            <div class="stat-label"><i data-lucide="hard-drive" class="icon-xs"></i> Compute_Node</div>
            <div class="stat-value">AIDP_GPU</div>
        </div>
    </div>

    <main class="main-view">
        <div class="alerts-panel">
            <div class="alerts-list">
                <div class="search-bar">
                    <input type="text" class="search-input" id="search-input" placeholder="SEARCH_PATTERNS // Type pattern name or category..." oninput="filterAlerts()">
                </div>
                <div class="alerts-content" id="alerts-container"></div>
            </div>
        </div>

        <div class="analytics-panel">
            <div class="chart-container" style="height: 55%;">
                <div class="chart-header">
                    <i data-lucide="activity" class="icon-xs"></i>
                    PATTERN_DETECTION_TIMELINE
                </div>
                <div class="chart-wrapper">
                    <canvas id="lineChart"></canvas>
                </div>
            </div>
            <div class="chart-container" style="height: 45%;">
                <div class="chart-header">
                    <i data-lucide="pie-chart" class="icon-xs"></i>
                    CATEGORY_DISTRIBUTION
                </div>
                <div class="chart-wrapper">
                    <canvas id="pieChart"></canvas>
                </div>
            </div>
        </div>
    </main>

    <div class="drawer" id="detail-drawer">
        <div style="display:flex; justify-content: space-between; align-items: center; margin-bottom: 24px;">
            <h2 id="drawer-title" style="font-size: 14px; letter-spacing: 2px;">INSPECTOR</h2>
            <button onclick="closeDrawer()" style="background:none; border:none; color:var(--text-dim); cursor:pointer; font-family:'JetBrains Mono'; font-size:11px;">[ ESC ]</button>
        </div>
        <div id="drawer-content">
            <div class="stat-label">Raw_Packet_Data</div>
            <pre class="json-block" id="drawer-json"></pre>
        </div>
    </div>

    <script>
        var currentAlerts = [];
        var allAlerts = [];
        var currentRPC = 'devnet';
        var lineChart = null;
        var pieChart = null;
        var timelineData = {
            labels: [],
            datasets: {}
        };
        var categoryData = {};
        var maxDataPoints = 20;

        function initCharts() {
            var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
            var gridColor = isDark ? '#262626' : '#e5e5e5';
            var textColor = isDark ? '#737373' : '#737373';
            var accentColor = isDark ? '#00ff41' : '#2563eb';

            // Line Chart
            var lineCtx = document.getElementById('lineChart').getContext('2d');
            lineChart = new Chart(lineCtx, {
                type: 'line',
                data: {
                    labels: [],
                    datasets: []
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    interaction: {
                        mode: 'index',
                        intersect: false,
                    },
                    plugins: {
                        legend: {
                            display: true,
                            position: 'top',
                            labels: {
                                color: textColor,
                                font: {
                                    family: 'JetBrains Mono',
                                    size: 10
                                },
                                boxWidth: 12,
                                boxHeight: 12
                            }
                        }
                    },
                    scales: {
                        x: {
                            grid: { color: gridColor },
                            ticks: {
                                color: textColor,
                                font: { family: 'JetBrains Mono', size: 9 }
                            }
                        },
                        y: {
                            grid: { color: gridColor },
                            ticks: {
                                color: textColor,
                                font: { family: 'JetBrains Mono', size: 9 }
                            },
                            beginAtZero: true
                        }
                    }
                }
            });

            // Pie Chart
            var pieCtx = document.getElementById('pieChart').getContext('2d');
            pieChart = new Chart(pieCtx, {
                type: 'doughnut',
                data: {
                    labels: [],
                    datasets: [{
                        data: [],
                        backgroundColor: [
                            accentColor,
                            '#f59e0b',
                            '#8b5cf6',
                            '#ec4899',
                            '#06b6d4',
                            '#10b981',
                            '#ef4444',
                            '#6366f1'
                        ],
                        borderWidth: 0
                    }]
                },
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: {
                        legend: {
                            display: true,
                            position: 'right',
                            labels: {
                                color: textColor,
                                font: {
                                    family: 'JetBrains Mono',
                                    size: 10
                                },
                                boxWidth: 12,
                                boxHeight: 12
                            }
                        }
                    }
                }
            });
        }

        function updateCharts() {
            if (!lineChart || !pieChart) return;

            // Get current time label
            var now = new Date();
            var timeLabel = now.getHours() + ':' + String(now.getMinutes()).padStart(2, '0') + ':' + String(now.getSeconds()).padStart(2, '0');

            // Count patterns in recent alerts (last 100)
            var recentAlerts = allAlerts.slice(0, 100);
            var patternCounts = {};
            var categoryCounts = {};

            recentAlerts.forEach(function(alert) {
                var pattern = alert.Result.PatternName;
                patternCounts[pattern] = (patternCounts[pattern] || 0) + 1;

                // Determine category
                var category = 'Other';
                if (pattern.includes('Token') || pattern.includes('Pump.fun')) category = 'Token';
                else if (pattern.includes('MEV') || pattern.includes('Whale')) category = 'MEV';
                else if (pattern.includes('Raydium') || pattern.includes('Orca') || pattern.includes('Jupiter') || pattern.includes('Flash')) category = 'DeFi';
                else if (pattern.includes('NFT') || pattern.includes('Magic Eden')) category = 'NFT';
                else if (pattern.includes('Failed')) category = 'Error';

                categoryCounts[category] = (categoryCounts[category] || 0) + 1;
            });

            // Update line chart
            if (timelineData.labels.length >= maxDataPoints) {
                timelineData.labels.shift();
                Object.keys(timelineData.datasets).forEach(function(key) {
                    timelineData.datasets[key].shift();
                });
            }

            timelineData.labels.push(timeLabel);
            Object.keys(patternCounts).forEach(function(pattern) {
                if (!timelineData.datasets[pattern]) {
                    timelineData.datasets[pattern] = [];
                }
            });

            // Add current counts
            var topPatterns = Object.keys(patternCounts).slice(0, 5); // Top 5 patterns
            topPatterns.forEach(function(pattern) {
                if (!timelineData.datasets[pattern]) {
                    timelineData.datasets[pattern] = new Array(timelineData.labels.length - 1).fill(0);
                }
                timelineData.datasets[pattern].push(patternCounts[pattern]);
            });

            // Pad datasets with zeros
            Object.keys(timelineData.datasets).forEach(function(pattern) {
                while (timelineData.datasets[pattern].length < timelineData.labels.length) {
                    timelineData.datasets[pattern].unshift(0);
                }
            });

            // Convert to Chart.js format
            var datasets = [];
            var colors = ['#00ff41', '#f59e0b', '#8b5cf6', '#ec4899', '#06b6d4'];
            var idx = 0;
            Object.keys(timelineData.datasets).forEach(function(pattern) {
                datasets.push({
                    label: pattern,
                    data: timelineData.datasets[pattern],
                    borderColor: colors[idx % colors.length],
                    backgroundColor: 'transparent',
                    borderWidth: 2,
                    tension: 0.3,
                    pointRadius: 2
                });
                idx++;
            });

            lineChart.data.labels = timelineData.labels;
            lineChart.data.datasets = datasets;
            lineChart.update('none');

            // Update pie chart
            pieChart.data.labels = Object.keys(categoryCounts);
            pieChart.data.datasets[0].data = Object.values(categoryCounts);
            pieChart.update('none');
        }

        function toggleTheme() {
            var root = document.documentElement;
            var current = root.getAttribute('data-theme');
            root.setAttribute('data-theme', current === 'dark' ? 'light' : 'dark');
        }

        function switchRPC(mode) {
            fetch('/api/switch-rpc', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ mode: mode })
            })
            .then(function(r) { return r.json(); })
            .then(function(data) {
                currentRPC = mode;
                document.getElementById('rpc-indicator').textContent = 'RPC: ' + mode.toUpperCase();
                document.getElementById('btn-devnet').classList.toggle('active', mode === 'devnet');
                document.getElementById('btn-mainnet').classList.toggle('active', mode === 'mainnet');
                console.log('Switched to ' + mode);
            })
            .catch(function(err) {
                console.error('Failed to switch RPC:', err);
            });
        }

        function updateDashboard() {
            fetch('/api/stats')
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    document.getElementById('total-logs').textContent = data.TotalLogs.toLocaleString();
                    document.getElementById('total-alerts').textContent = data.TotalAlerts.toLocaleString();
                    document.getElementById('processing-time').textContent = (data.ProcessingTime / 1000000).toFixed(2);
                });

            fetch('/api/alerts')
                .then(function(r) { return r.json(); })
                .then(function(alerts) {
                    allAlerts = alerts || [];
                    renderAlerts(allAlerts);
                    updateCharts();
                });

            fetch('/api/rpc-mode')
                .then(function(r) { return r.json(); })
                .then(function(data) {
                    if (data.mode !== currentRPC) {
                        switchRPC(data.mode);
                    }
                });
        }

        function filterAlerts() {
            var query = document.getElementById('search-input').value.toLowerCase();
            if (!query) {
                renderAlerts(allAlerts);
                return;
            }

            var filtered = allAlerts.filter(function(alert) {
                var patternName = alert.Result.PatternName.toLowerCase();
                var message = alert.Message.toLowerCase();
                return patternName.includes(query) || message.includes(query);
            });

            renderAlerts(filtered);
        }

        function renderAlerts(alerts) {
            currentAlerts = alerts;
            var container = document.getElementById('alerts-container');
            
            if (alerts.length === 0) {
                container.innerHTML = '<div style="text-align: center; padding: 40px; color: var(--text-dim); font-family: JetBrains Mono;">NO_SIGNALS_DETECTED</div>';
                return;
            }

            var html = "";
            for (var i = 0; i < alerts.length; i++) {
                var alert = alerts[i];
                var time = new Date(alert.Timestamp).toLocaleTimeString();
                var confidence = (alert.Result.Confidence * 100).toFixed(1);
                
                html += '<div class="alert-card" onclick="showDetails(' + alert.ID + ')">' +
                        '<div class="alert-header">' +
                            '<span class="pattern-tag">' + alert.Result.PatternName + '</span>' +
                            '<span style="font-family: \'JetBrains Mono\'; font-size: 11px; color: var(--text-dim)">' + time + '</span>' +
                        '</div>' +
                        '<div style="font-size: 13px; margin-bottom: 12px; font-weight: 500;">' + alert.Message + '</div>' +
                        '<div style="display: flex; align-items: center; gap: 8px; font-size: 10px; color: var(--accent); font-weight: 800;">' +
                            '<i data-lucide="shield-check" class="icon-xs"></i>' +
                            'CONFIDENCE: ' + confidence + '%' +
                        '</div>' +
                    '</div>';
            }
            container.innerHTML = html;
            if (window.lucide) {
                window.lucide.createIcons();
            }
        }

        function showDetails(id) {
            var alert = currentAlerts.find(function(a) { return a.ID === id; });
            if (!alert) return;
            document.getElementById('drawer-title').textContent = alert.Result.PatternName.toUpperCase();
            document.getElementById('drawer-json').textContent = JSON.stringify(alert.Result, null, 2);
            document.getElementById('detail-drawer').classList.add('open');
        }

        function closeDrawer() {
            document.getElementById('detail-drawer').classList.remove('open');
        }

        updateDashboard();
        setInterval(updateDashboard, 2000);
        window.onload = function() {
            initCharts();
            if (window.lucide) {
                window.lucide.createIcons();
            }
        };
    </script>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	t, _ := template.New("dashboard").Parse(tmpl)
	t.Execute(w, nil)
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.alerts)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.stats)
}

func (s *Server) handleRPCMode(w http.ResponseWriter, r *http.Request) {
	s.rpcMu.RLock()
	defer s.rpcMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"mode": s.rpcMode})
}

func (s *Server) handleSwitchRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if req.Mode != "devnet" && req.Mode != "mainnet" {
		http.Error(w, "Invalid mode", http.StatusBadRequest)
		return
	}

	s.rpcMu.Lock()
	s.rpcMode = req.Mode
	s.rpcMu.Unlock()

	log.Printf("🔄 RPC mode switched to: %s", req.Mode)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "success", "mode": req.Mode})
}

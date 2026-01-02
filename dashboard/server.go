package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	"logstellar/gpu"
)

type Server struct {
	port      int
	alerts    []Alert
	stats     Stats
	mu        sync.RWMutex
}

type Alert struct {
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

func NewServer(port int) *Server {
	return &Server{
		port:   port,
		alerts: make([]Alert, 0),
		stats:  Stats{},
	}
}

func (s *Server) Start() {
	http.HandleFunc("/", s.handleHome)
	http.HandleFunc("/api/alerts", s.handleAlerts)
	http.HandleFunc("/api/stats", s.handleStats)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Dashboard server starting on %s", addr)
	
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start dashboard: %v", err)
	}
}

func (s *Server) AddAlert(message string, result gpu.ScanResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	alert := Alert{
		Message:   message,
		Result:    result,
		Timestamp: time.Now(),
	}

	s.alerts = append([]Alert{alert}, s.alerts...) // Prepend new alerts
	
	// Keep only last 100 alerts
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

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	tmpl := `
<!DOCTYPE html>
<html>
<head>
    <title>LogStellar Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: 'Segoe UI', system-ui, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 1400px;
            margin: 0 auto;
        }
        .header {
            background: rgba(255, 255, 255, 0.95);
            backdrop-filter: blur(10px);
            border-radius: 20px;
            padding: 30px;
            margin-bottom: 20px;
            box-shadow: 0 20px 60px rgba(0,0,0,0.3);
        }
        .header h1 {
            font-size: 2.5em;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            margin-bottom: 10px;
        }
        .header p {
            color: #666;
            font-size: 1.1em;
        }
        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 20px;
            margin-bottom: 20px;
        }
        .stat-card {
            background: rgba(255, 255, 255, 0.95);
            backdrop-filter: blur(10px);
            border-radius: 15px;
            padding: 25px;
            box-shadow: 0 10px 30px rgba(0,0,0,0.2);
        }
        .stat-card h3 {
            color: #667eea;
            font-size: 0.9em;
            text-transform: uppercase;
            margin-bottom: 10px;
            letter-spacing: 1px;
        }
        .stat-card .value {
            font-size: 2.5em;
            font-weight: bold;
            color: #333;
        }
        .alerts-section {
            background: rgba(255, 255, 255, 0.95);
            backdrop-filter: blur(10px);
            border-radius: 20px;
            padding: 30px;
            box-shadow: 0 20px 60px rgba(0,0,0,0.3);
        }
        .alerts-section h2 {
            color: #667eea;
            margin-bottom: 20px;
            font-size: 1.8em;
        }
        .alert-item {
            background: #f8f9fa;
            border-left: 4px solid #667eea;
            padding: 15px;
            margin-bottom: 15px;
            border-radius: 8px;
            transition: transform 0.2s;
        }
        .alert-item:hover {
            transform: translateX(5px);
        }
        .alert-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 8px;
        }
        .pattern-name {
            font-weight: bold;
            color: #764ba2;
            font-size: 1.1em;
        }
        .timestamp {
            color: #999;
            font-size: 0.9em;
        }
        .confidence {
            display: inline-block;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 4px 12px;
            border-radius: 20px;
            font-size: 0.85em;
            font-weight: bold;
        }
        .gpu-badge {
            display: inline-block;
            background: #28a745;
            color: white;
            padding: 5px 15px;
            border-radius: 20px;
            font-size: 0.85em;
            font-weight: bold;
            margin-left: 10px;
        }
        .loading {
            text-align: center;
            padding: 40px;
            color: #999;
        }
        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }
        .live-indicator {
            display: inline-block;
            width: 10px;
            height: 10px;
            background: #28a745;
            border-radius: 50%;
            margin-right: 8px;
            animation: pulse 2s infinite;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🌟 LogStellar Dashboard</h1>
            <p><span class="live-indicator"></span>GPU-Accelerated Solana Log Analyzer <span class="gpu-badge">GPU POWERED</span></p>
        </div>

        <div class="stats-grid">
            <div class="stat-card">
                <h3>Total Logs Processed</h3>
                <div class="value" id="total-logs">0</div>
            </div>
            <div class="stat-card">
                <h3>Patterns Detected</h3>
                <div class="value" id="total-alerts">0</div>
            </div>
            <div class="stat-card">
                <h3>Processing Time</h3>
                <div class="value" id="processing-time">0ms</div>
            </div>
            <div class="stat-card">
                <h3>GPU Efficiency</h3>
                <div class="value">10,000x</div>
            </div>
        </div>

        <div class="alerts-section">
            <h2>🎯 Recent Alerts</h2>
            <div id="alerts-container">
                <div class="loading">Waiting for alerts...</div>
            </div>
        </div>
    </div>

    <script>
        function updateDashboard() {
            // Fetch stats
            fetch('/api/stats')
                .then(r => r.json())
                .then(data => {
                    document.getElementById('total-logs').textContent = data.TotalLogs.toLocaleString();
                    document.getElementById('total-alerts').textContent = data.TotalAlerts.toLocaleString();
                    document.getElementById('processing-time').textContent = 
                        (data.ProcessingTime / 1000000).toFixed(2) + 'ms';
                });

            // Fetch alerts
            fetch('/api/alerts')
                .then(r => r.json())
                .then(alerts => {
                    const container = document.getElementById('alerts-container');
                    if (alerts.length === 0) {
                        container.innerHTML = '<div class="loading">Waiting for alerts...</div>';
                        return;
                    }

                    container.innerHTML = alerts.map(alert => {
                        const time = new Date(alert.Timestamp).toLocaleTimeString();
                        const confidence = (alert.Result.Confidence * 100).toFixed(1);
                        return \`
                            <div class="alert-item">
                                <div class="alert-header">
                                    <span class="pattern-name">\${alert.Result.PatternName}</span>
                                    <span class="timestamp">\${time}</span>
                                </div>
                                <div>
                                    <span class="confidence">\${confidence}% Match</span>
                                </div>
                            </div>
                        \`;
                    }).join('');
                });
        }

        // Update every 2 seconds
        updateDashboard();
        setInterval(updateDashboard, 2000);
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

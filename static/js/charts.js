/**
 * Migration Dashboard Charts
 * Provides interactive data visualizations for the GitHub migration dashboard
 */

// Chart color schemes based on our design system
const CHART_COLORS = {
    primary: '#0366d6',
    primaryLight: '#cce7ff',
    success: '#2ea44f',
    successLight: '#dcffe4',
    warning: '#d29922',
    warningLight: '#fff8dc',
    danger: '#cb2431',
    dangerLight: '#ffe3e6',
    info: '#0969da',
    infoLight: '#ddf4ff',
    gray: '#656d76',
    grayLight: '#f6f8fa'
};

// Chart configuration defaults
const CHART_DEFAULTS = {
    responsive: true,
    maintainAspectRatio: false,
    plugins: {
        legend: {
            position: 'bottom',
            labels: {
                usePointStyle: true,
                padding: 20,
                font: {
                    size: 12,
                    family: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
                }
            }
        },
        tooltip: {
            backgroundColor: 'rgba(0, 0, 0, 0.8)',
            titleColor: '#ffffff',
            bodyColor: '#ffffff',
            borderColor: CHART_COLORS.primary,
            borderWidth: 1,
            cornerRadius: 8,
            padding: 12
        }
    },
    scales: {
        x: {
            grid: {
                color: 'rgba(0, 0, 0, 0.05)'
            },
            ticks: {
                font: {
                    size: 11,
                    family: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
                }
            }
        },
        y: {
            grid: {
                color: 'rgba(0, 0, 0, 0.05)'
            },
            ticks: {
                font: {
                    size: 11,
                    family: '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
                }
            }
        }
    }
};

/**
 * Migration Status Distribution Chart
 */
function createStatusDistributionChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    const chartData = {
        labels: ['Succeeded', 'Running', 'Failed', 'Pending'],
        datasets: [{
            data: [
                data.succeeded || 0,
                data.running || 0,
                data.failed || 0,
                data.pending || 0
            ],
            backgroundColor: [
                CHART_COLORS.success,
                CHART_COLORS.warning,
                CHART_COLORS.danger,
                CHART_COLORS.gray
            ],
            borderColor: [
                CHART_COLORS.success,
                CHART_COLORS.warning,
                CHART_COLORS.danger,
                CHART_COLORS.gray
            ],
            borderWidth: 2,
            hoverOffset: 8
        }]
    };

    return new Chart(ctx, {
        type: 'doughnut',
        data: chartData,
        options: {
            ...CHART_DEFAULTS,
            plugins: {
                ...CHART_DEFAULTS.plugins,
                legend: {
                    display: false // Disable Chart.js legend since we have a custom HTML legend
                }
            },
            cutout: '60%'
        }
    });
}

/**
 * Migration Trends Over Time Chart
 */
function createMigrationTrendsChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    const chartData = {
        labels: data.labels || [],
        datasets: [
            {
                label: 'Successful Migrations',
                data: data.successful || [],
                borderColor: CHART_COLORS.success,
                backgroundColor: CHART_COLORS.successLight,
                fill: true,
                tension: 0.4
            },
            {
                label: 'Failed Migrations',
                data: data.failed || [],
                borderColor: CHART_COLORS.danger,
                backgroundColor: CHART_COLORS.dangerLight,
                fill: true,
                tension: 0.4
            },
            {
                label: 'Total Migrations',
                data: data.total || [],
                borderColor: CHART_COLORS.primary,
                backgroundColor: 'transparent',
                borderWidth: 3,
                pointBackgroundColor: CHART_COLORS.primary,
                pointBorderColor: '#ffffff',
                pointBorderWidth: 2,
                pointRadius: 4
            }
        ]
    };

    return new Chart(ctx, {
        type: 'line',
        data: chartData,
        options: {
            ...CHART_DEFAULTS,
            scales: {
                ...CHART_DEFAULTS.scales,
                y: {
                    ...CHART_DEFAULTS.scales.y,
                    beginAtZero: true
                }
            }
        }
    });
}

/**
 * Repository Size Distribution Chart
 */
function createSizeDistributionChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    const chartData = {
        labels: ['Small', 'Medium', 'Large', 'Extra Large'],
        datasets: [{
            label: 'Repository Count',
            data: [
                data.small || 0,
                data.medium || 0,
                data.large || 0,
                data.extraLarge || 0
            ],
            backgroundColor: [
                CHART_COLORS.success,
                CHART_COLORS.warning,
                CHART_COLORS.danger,
                CHART_COLORS.gray
            ],
            borderColor: [
                CHART_COLORS.success,
                CHART_COLORS.warning,
                CHART_COLORS.danger,
                CHART_COLORS.gray
            ],
            borderWidth: 1,
            borderRadius: 4,
            borderSkipped: false
        }]
    };

    return new Chart(ctx, {
        type: 'bar',
        data: chartData,
        options: {
            ...CHART_DEFAULTS,
            scales: {
                ...CHART_DEFAULTS.scales,
                y: {
                    ...CHART_DEFAULTS.scales.y,
                    beginAtZero: true
                }
            }
        }
    });
}

/**
 * Migration Performance Metrics Chart
 */
function createPerformanceChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    const chartData = {
        labels: data.labels || [],
        datasets: [
            {
                label: 'Average Duration (hours)',
                data: data.duration || [],
                backgroundColor: CHART_COLORS.primary,
                borderColor: CHART_COLORS.primary,
                borderWidth: 1,
                yAxisID: 'y'
            },
            {
                label: 'Success Rate (%)',
                data: data.successRate || [],
                type: 'line',
                borderColor: CHART_COLORS.success,
                backgroundColor: 'transparent',
                borderWidth: 3,
                pointBackgroundColor: CHART_COLORS.success,
                pointBorderColor: '#ffffff',
                pointBorderWidth: 2,
                pointRadius: 5,
                yAxisID: 'y1'
            }
        ]
    };

    return new Chart(ctx, {
        type: 'bar',
        data: chartData,
        options: {
            ...CHART_DEFAULTS,
            scales: {
                x: CHART_DEFAULTS.scales.x,
                y: {
                    ...CHART_DEFAULTS.scales.y,
                    type: 'linear',
                    display: true,
                    position: 'left',
                    beginAtZero: true,
                    title: {
                        display: true,
                        text: 'Duration (hours)'
                    }
                },
                y1: {
                    ...CHART_DEFAULTS.scales.y,
                    type: 'linear',
                    display: true,
                    position: 'right',
                    min: 0,
                    max: 100,
                    title: {
                        display: true,
                        text: 'Success Rate (%)'
                    },
                    grid: {
                        drawOnChartArea: false,
                    }
                }
            }
        }
    });
}

/**
 * Real-time Migration Activity Heatmap
 */
function createActivityHeatmap(containerId, data) {
    const container = document.getElementById(containerId);
    if (!container) return null;

    // Create heatmap grid for 24 hours x 7 days
    const hours = Array.from({length: 24}, (_, i) => i);
    const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
    
    let html = '<div class="heatmap-container">';
    html += '<div class="heatmap-grid">';
    
    // Time labels
    html += '<div class="time-labels">';
    hours.forEach(hour => {
        html += `<div class="time-label">${hour.toString().padStart(2, '0')}:00</div>`;
    });
    html += '</div>';
    
    // Day rows
    days.forEach((day, dayIndex) => {
        html += `<div class="day-row">`;
        html += `<div class="day-label">${day}</div>`;
        
        hours.forEach(hour => {
            const activity = (data.heatmap && data.heatmap[dayIndex] && data.heatmap[dayIndex][hour]) || 0;
            const intensity = Math.min(activity / (data.maxActivity || 1), 1);
            const opacity = Math.max(intensity, 0.1);
            
            html += `<div class="heatmap-cell" 
                style="background-color: rgba(3, 102, 214, ${opacity})" 
                data-activity="${activity}" 
                data-day="${day}" 
                data-hour="${hour}">
            </div>`;
        });
        
        html += '</div>';
    });
    
    html += '</div></div>';
    container.innerHTML = html;
    
    // Add tooltips
    container.querySelectorAll('.heatmap-cell').forEach(cell => {
        cell.addEventListener('mouseenter', (e) => {
            const activity = e.target.dataset.activity;
            const day = e.target.dataset.day;
            const hour = e.target.dataset.hour;
            
            // Create tooltip (you can enhance this further)
            e.target.title = `${day} ${hour}:00 - ${activity} migrations`;
        });
    });
}

/**
 * Initialize all dashboard charts
 */
function initializeDashboardCharts() {
    // Wait for DOM to be ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initializeDashboardCharts);
        return;
    }

    // Fetch chart data and initialize charts
    fetchChartData().then(data => {
        // Initialize status distribution chart
        if (document.getElementById('statusChart')) {
            createStatusDistributionChart('statusChart', data.status);
        }
        
        // Initialize migration trends chart
        if (document.getElementById('trendsChart')) {
            createMigrationTrendsChart('trendsChart', data.trends);
        }
        
        // Initialize size distribution chart
        if (document.getElementById('sizeChart')) {
            createSizeDistributionChart('sizeChart', data.sizes);
        }
        
        // Initialize performance chart
        if (document.getElementById('performanceChart')) {
            createPerformanceChart('performanceChart', data.performance);
        }
        
        // Initialize activity heatmap
        if (document.getElementById('activityHeatmap')) {
            createActivityHeatmap('activityHeatmap', data.activity);
        }
    }).catch(error => {
        console.error('Failed to initialize charts:', error);
    });
}

/**
 * Fetch chart data from the server
 */
async function fetchChartData() {
    try {
        const response = await fetch('/dashboard/chart-data');
        if (!response.ok) {
            throw new Error('Failed to fetch chart data');
        }
        return await response.json();
    } catch (error) {
        console.error('Error fetching chart data:', error);
        // Return empty data structure instead of mock data
        return {
            status: {
                succeeded: 0,
                running: 0,
                failed: 0,
                pending: 0
            },
            trends: {
                labels: [],
                successful: [],
                failed: [],
                total: []
            },
            sizes: {
                small: 0,
                medium: 0,
                large: 0,
                extraLarge: 0
            },
            performance: {
                labels: [],
                duration: [],
                successRate: []
            },
            activity: {
                maxActivity: 0,
                heatmap: Array.from({length: 7}, () => Array(24).fill(0))
            }
        };
    }
}

/**
 * Refresh charts with new data
 */
function refreshCharts() {
    // This function can be called to update charts with new data
    fetchChartData().then(data => {
        // Update existing charts
        Object.values(Chart.instances).forEach(chart => {
            if (chart.canvas.id === 'statusChart') {
                chart.data.datasets[0].data = [
                    data.status.succeeded,
                    data.status.running,
                    data.status.failed,
                    data.status.pending
                ];
                chart.update('none');
            }
            // Add more chart updates as needed
        });
    });
}

// Auto-initialize charts when the script loads
initializeDashboardCharts();

// Export functions for external use
window.DashboardCharts = {
    createStatusDistributionChart,
    createMigrationTrendsChart,
    createSizeDistributionChart,
    createPerformanceChart,
    createActivityHeatmap,
    refreshCharts,
    initializeDashboardCharts
}; 
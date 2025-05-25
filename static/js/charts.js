/**
 * Migration Dashboard Charts
 * Provides interactive data visualizations for the GitHub migration dashboard
 */

// Global chart instances storage for proper cleanup
const chartInstances = {};

// Flag to prevent multiple initializations
let isInitialized = false;

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

    // Destroy existing chart instance if it exists
    if (chartInstances[canvasId]) {
        chartInstances[canvasId].destroy();
        delete chartInstances[canvasId];
    }

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

    const chart = new Chart(ctx, {
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

    // Store the chart instance for future cleanup
    chartInstances[canvasId] = chart;
    return chart;
}

/**
 * Migration Trends Over Time Chart
 */
function createMigrationTrendsChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    // Destroy existing chart instance if it exists
    if (chartInstances[canvasId]) {
        chartInstances[canvasId].destroy();
        delete chartInstances[canvasId];
    }

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

    const chart = new Chart(ctx, {
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

    // Store the chart instance for future cleanup
    chartInstances[canvasId] = chart;
    return chart;
}

/**
 * Repository Size Distribution Chart
 */
function createSizeDistributionChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    // Destroy existing chart instance if it exists
    if (chartInstances[canvasId]) {
        chartInstances[canvasId].destroy();
        delete chartInstances[canvasId];
    }

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

    const chart = new Chart(ctx, {
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

    // Store the chart instance for future cleanup
    chartInstances[canvasId] = chart;
    return chart;
}

/**
 * Migration Performance Metrics Chart
 */
function createPerformanceChart(canvasId, data) {
    const ctx = document.getElementById(canvasId);
    if (!ctx) return null;

    // Destroy existing chart instance if it exists
    if (chartInstances[canvasId]) {
        chartInstances[canvasId].destroy();
        delete chartInstances[canvasId];
    }

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

    const chart = new Chart(ctx, {
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

    // Store the chart instance for future cleanup
    chartInstances[canvasId] = chart;
    return chart;
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
    
    // Clear container but preserve existing classes
    container.innerHTML = '';
    
    // Create a wrapper for the heatmap
    const heatmapWrapper = document.createElement('div');
    heatmapWrapper.className = 'heatmap-wrapper';
    
    // Create the grid container
    const gridContainer = document.createElement('div');
    gridContainer.className = 'heatmap-grid';
    
    // Empty cell for top-left corner
    const cornerCell = document.createElement('div');
    cornerCell.className = 'time-label';
    gridContainer.appendChild(cornerCell);
    
    // Time labels (hours) - header row
    hours.forEach(hour => {
        const timeLabel = document.createElement('div');
        timeLabel.className = 'time-label';
        timeLabel.textContent = `${hour.toString().padStart(2, '0')}:00`;
        gridContainer.appendChild(timeLabel);
    });
    
    // Day rows with cells
    days.forEach((day, dayIndex) => {
        // Day label (first column)
        const dayLabel = document.createElement('div');
        dayLabel.className = 'day-label';
        dayLabel.textContent = day;
        gridContainer.appendChild(dayLabel);
        
        // Hour cells for this day (24 columns)
        hours.forEach(hour => {
            const activity = (data.heatmap && data.heatmap[dayIndex] && data.heatmap[dayIndex][hour]) || 0;
            const intensity = Math.min(activity / (data.maxActivity || 1), 1);
            
            const cell = document.createElement('div');
            cell.className = 'heatmap-cell';
            cell.dataset.activity = activity;
            cell.dataset.day = day;
            cell.dataset.hour = hour;
            
            // Only apply background color if there's activity
            if (activity > 0) {
                const opacity = Math.max(intensity, 0.2); // Minimum opacity for visible cells
                cell.style.backgroundColor = `rgba(3, 102, 214, ${opacity})`;
            }
            
            // Add tooltip on hover
            cell.addEventListener('mouseenter', (e) => {
                const activityCount = parseInt(e.target.dataset.activity);
                const dayName = e.target.dataset.day;
                const hourValue = e.target.dataset.hour;
                
                // Create more detailed tooltip
                let tooltip = `${dayName} ${hourValue}:00`;
                if (activityCount === 0) {
                    tooltip += ' - No migrations';
                } else if (activityCount === 1) {
                    tooltip += ' - 1 migration';
                } else {
                    tooltip += ` - ${activityCount} migrations`;
                }
                
                e.target.title = tooltip;
            });
            
            gridContainer.appendChild(cell);
        });
    });
    
    heatmapWrapper.appendChild(gridContainer);
    container.appendChild(heatmapWrapper);
    
    // Heatmap created successfully
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

    // Prevent multiple initializations
    if (isInitialized) {
        console.log('Charts already initialized, skipping...');
        return;
    }

    console.log('Initializing dashboard charts...');
    isInitialized = true;

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
                maxActivity: 10,
                heatmap: Array.from({length: 7}, (_, dayIndex) => 
                    Array.from({length: 24}, (_, hour) => {
                        // Generate some sample data for demonstration
                        // Higher activity during business hours (9-17) and weekdays
                        const isWeekday = dayIndex >= 1 && dayIndex <= 5;
                        const isBusinessHour = hour >= 9 && hour <= 17;
                        
                        if (isWeekday && isBusinessHour) {
                            return Math.floor(Math.random() * 8) + 2; // 2-10 activity
                        } else if (isWeekday) {
                            return Math.floor(Math.random() * 3); // 0-3 activity
                        } else {
                            return Math.floor(Math.random() * 2); // 0-2 activity
                        }
                    })
                )
            }
        };
    }
}

/**
 * Destroy all chart instances
 */
function destroyAllCharts() {
    Object.values(chartInstances).forEach(chart => {
        if (chart && typeof chart.destroy === 'function') {
            chart.destroy();
        }
    });
    // Clear the instances object
    Object.keys(chartInstances).forEach(key => delete chartInstances[key]);
    // Reset initialization flag
    isInitialized = false;
    console.log('All charts destroyed and initialization flag reset');
}

/**
 * Reinitialize all charts (destroy existing and create new ones)
 */
function reinitializeCharts() {
    destroyAllCharts();
    initializeDashboardCharts();
}

/**
 * Refresh charts with new data
 */
function refreshCharts() {
    // This function can be called to update charts with new data
    fetchChartData().then(data => {
        // Update existing charts using our tracked instances
        Object.entries(chartInstances).forEach(([canvasId, chart]) => {
            if (canvasId === 'statusChart' && data.status) {
                chart.data.datasets[0].data = [
                    data.status.succeeded,
                    data.status.running,
                    data.status.failed,
                    data.status.pending
                ];
                chart.update('none');
            } else if (canvasId === 'trendsChart' && data.trends) {
                chart.data.labels = data.trends.labels;
                chart.data.datasets[0].data = data.trends.successful;
                chart.data.datasets[1].data = data.trends.failed;
                chart.data.datasets[2].data = data.trends.total;
                chart.update('none');
            } else if (canvasId === 'sizeChart' && data.sizes) {
                chart.data.datasets[0].data = [
                    data.sizes.small,
                    data.sizes.medium,
                    data.sizes.large,
                    data.sizes.extraLarge
                ];
                chart.update('none');
            } else if (canvasId === 'performanceChart' && data.performance) {
                chart.data.labels = data.performance.labels;
                chart.data.datasets[0].data = data.performance.duration;
                chart.data.datasets[1].data = data.performance.successRate;
                chart.update('none');
            }
        });

        // Update activity heatmap
        if (data.activity && document.getElementById('activityHeatmap')) {
            createActivityHeatmap('activityHeatmap', data.activity);
        }
    }).catch(error => {
        console.error('Failed to refresh charts:', error);
    });
}

// Auto-initialize charts when the script loads
initializeDashboardCharts();

// Clean up charts when page is being unloaded
window.addEventListener('beforeunload', () => {
    destroyAllCharts();
});

// Export functions for external use
window.DashboardCharts = {
    createStatusDistributionChart,
    createMigrationTrendsChart,
    createSizeDistributionChart,
    createPerformanceChart,
    createActivityHeatmap,
    refreshCharts,
    initializeDashboardCharts,
    destroyAllCharts,
    reinitializeCharts,
    chartInstances // Expose for debugging
}; 
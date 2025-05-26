/**
 * Enhanced Progress Indicator
 * Provides advanced progress visualization for GitHub migrations
 */

class EnhancedProgressIndicator {
    constructor(containerId, options = {}) {
        this.container = document.getElementById(containerId);
        this.options = {
            showStages: true,
            showETA: true,
            animateProgress: true,
            updateInterval: 5000,
            ...options
        };
        
        this.stages = [
            { key: 'validation', name: 'Validation', description: 'Validating repository access' },
            { key: 'setup', name: 'Setup', description: 'Preparing migration environment' },
            { key: 'archive', name: 'Archive', description: 'Creating repository archive' },
            { key: 'storage', name: 'Storage', description: 'Uploading to secure storage' },
            { key: 'migration', name: 'Migration', description: 'Migrating to destination' }
        ];
        
        this.init();
    }
    
    init() {
        if (!this.container) return;
        
        this.render();
        this.setupEventListeners();
        
        if (this.options.updateInterval) {
            this.startAutoUpdate();
        }
    }
    
    render() {
        const data = this.options.data || {};
        
        this.container.innerHTML = `
            <div class="enhanced-progress">
                ${this.options.showStages ? this.renderStageProgress(data) : ''}
                <div class="overall-progress">
                    <div class="progress-header">
                        <span class="progress-label">Overall Progress</span>
                        <span class="progress-percentage">${data.progress || 0}%</span>
                        ${this.options.showETA ? `<span class="progress-eta">${this.calculateETA(data)}</span>` : ''}
                    </div>
                    <div class="progress-bar-container">
                        <div class="progress-bar-track">
                            <div class="progress-bar-fill" style="width: ${data.progress || 0}%"></div>
                            <div class="progress-bar-glow"></div>
                        </div>
                    </div>
                    <div class="progress-details">
                        <span class="current-stage">${this.getCurrentStageText(data)}</span>
                        <span class="progress-speed">${this.calculateSpeed(data)}</span>
                    </div>
                </div>
            </div>
        `;
        
        if (this.options.animateProgress) {
            this.animateProgressBars();
        }
    }
    
    renderStageProgress(data) {
        const currentStage = data.stage || 'validation';
        const stageStatuses = data.stageStatuses || {};
        
        return `
            <div class="stage-progress">
                <div class="stage-header">
                    <h4>Migration Stages</h4>
                    <span class="stage-indicator">${this.getCompletedStagesCount(stageStatuses)}/${this.stages.length}</span>
                </div>
                <div class="stages-timeline">
                    ${this.stages.map((stage, index) => this.renderStage(stage, index, currentStage, stageStatuses)).join('')}
                </div>
            </div>
        `;
    }
    
    renderStage(stage, index, currentStage, stageStatuses) {
        const status = stageStatuses[stage.key] || this.getStageStatus(stage.key, currentStage);
        const isActive = stage.key === currentStage;
        const isCompleted = status === 'completed';
        const isFailed = status === 'failed';
        const isSkipped = status === 'skipped';
        
        return `
            <div class="stage-item ${status} ${isActive ? 'active' : ''}" data-stage="${stage.key}">
                <div class="stage-connector ${index === 0 ? 'first' : ''} ${index === this.stages.length - 1 ? 'last' : ''}"></div>
                <div class="stage-marker">
                    <div class="stage-icon">
                        ${this.getStageIcon(status, isActive)}
                    </div>
                    <div class="stage-pulse ${isActive && !isCompleted && !isFailed ? 'active' : ''}"></div>
                </div>
                <div class="stage-content">
                    <div class="stage-name">${stage.name}</div>
                    <div class="stage-description">${stage.description}</div>
                    <div class="stage-status-text">${this.getStageStatusText(status, isActive)}</div>
                </div>
            </div>
        `;
    }
    
    getStageIcon(status, isActive) {
        switch (status) {
            case 'completed':
                return '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>';
            case 'failed':
                return '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg>';
            case 'skipped':
                return '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>';
            case 'current':
                return isActive ? 
                    '<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg>' :
                    '<div class="stage-number">' + (this.stages.findIndex(s => s.key === status) + 1) + '</div>';
            default:
                return '<div class="stage-number">' + (this.stages.findIndex(s => s.key === status) + 1) + '</div>';
        }
    }
    
    getStageStatus(stageKey, currentStage) {
        const currentIndex = this.stages.findIndex(s => s.key === currentStage);
        const stageIndex = this.stages.findIndex(s => s.key === stageKey);
        
        if (stageIndex < currentIndex) return 'completed';
        if (stageIndex === currentIndex) return 'current';
        return 'pending';
    }
    
    getStageStatusText(status, isActive) {
        switch (status) {
            case 'completed': return 'Completed';
            case 'failed': return 'Failed';
            case 'skipped': return 'Skipped';
            case 'current': return isActive ? 'In Progress...' : 'Current';
            default: return 'Pending';
        }
    }
    
    getCompletedStagesCount(stageStatuses) {
        return Object.values(stageStatuses).filter(status => status === 'completed').length;
    }
    
    getCurrentStageText(data) {
        const stage = this.stages.find(s => s.key === data.stage);
        return stage ? `Current: ${stage.name}` : 'Initializing...';
    }
    
    calculateETA(data) {
        if (!data.startedAt || !data.progress || data.progress === 0) {
            return 'Calculating...';
        }
        
        const startTime = new Date(data.startedAt);
        const now = new Date();
        const elapsed = now - startTime;
        const rate = data.progress / elapsed;
        const remaining = (100 - data.progress) / rate;
        
        if (remaining < 60000) { // Less than 1 minute
            return '< 1 min remaining';
        } else if (remaining < 3600000) { // Less than 1 hour
            const minutes = Math.round(remaining / 60000);
            return `${minutes} min remaining`;
        } else {
            const hours = Math.round(remaining / 3600000);
            return `${hours}h remaining`;
        }
    }
    
    calculateSpeed(data) {
        if (!data.repositorySize || !data.progress || data.progress === 0) {
            return '';
        }
        
        const sizeBytes = data.repositorySize;
        const sizeMB = sizeBytes / (1024 * 1024);
        const completedMB = sizeMB * (data.progress / 100);
        
        if (!data.startedAt) return '';
        
        const startTime = new Date(data.startedAt);
        const now = new Date();
        const elapsedMinutes = (now - startTime) / (1000 * 60);
        
        if (elapsedMinutes > 0) {
            const mbPerMinute = completedMB / elapsedMinutes;
            if (mbPerMinute > 1) {
                return `${mbPerMinute.toFixed(1)} MB/min`;
            } else {
                return `${(mbPerMinute * 1024).toFixed(0)} KB/min`;
            }
        }
        
        return '';
    }
    
    animateProgressBars() {
        const fills = this.container.querySelectorAll('.progress-bar-fill');
        fills.forEach(fill => {
            const width = fill.style.width;
            fill.style.width = '0%';
            fill.style.transition = 'width 1s ease-out';
            
            setTimeout(() => {
                fill.style.width = width;
            }, 100);
        });
    }
    
    setupEventListeners() {
        // Add hover effects for stages
        const stageItems = this.container.querySelectorAll('.stage-item');
        stageItems.forEach(item => {
            item.addEventListener('mouseenter', () => {
                item.classList.add('hovered');
            });
            
            item.addEventListener('mouseleave', () => {
                item.classList.remove('hovered');
            });
        });
    }
    
    startAutoUpdate() {
        if (this.updateTimer) {
            clearInterval(this.updateTimer);
        }
        
        this.updateTimer = setInterval(() => {
            this.refresh();
        }, this.options.updateInterval);
    }
    
    refresh() {
        // Fetch updated data and re-render
        if (this.options.refreshCallback) {
            this.options.refreshCallback().then(data => {
                this.options.data = data;
                this.render();
            });
        }
    }
    
    update(data) {
        this.options.data = data;
        this.render();
    }
    
    destroy() {
        if (this.updateTimer) {
            clearInterval(this.updateTimer);
        }
        
        if (this.container) {
            this.container.innerHTML = '';
        }
    }
}

// Auto-initialize enhanced progress indicators
document.addEventListener('DOMContentLoaded', () => {
    // Look for elements with data-enhanced-progress attribute
    const progressElements = document.querySelectorAll('[data-enhanced-progress]');
    
    progressElements.forEach(element => {
        const options = {
            data: JSON.parse(element.dataset.progressData || '{}'),
            showStages: element.dataset.showStages !== 'false',
            showETA: element.dataset.showEta !== 'false',
            animateProgress: element.dataset.animate !== 'false'
        };
        
        new EnhancedProgressIndicator(element.id, options);
    });
});

// Export for global use
window.EnhancedProgressIndicator = EnhancedProgressIndicator; 
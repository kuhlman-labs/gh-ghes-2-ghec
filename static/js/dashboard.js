/**
 * Dashboard enhancement JavaScript
 * Provides client-side functionality for the migration dashboard
 */

document.addEventListener('DOMContentLoaded', function() {
    // Initialize progress bars
    initializeProgressBars();
    
    // Set up auto-refresh toggling
    setupAutoRefresh();
    
    // Set up sorting functionality
    setupSorting();
    
    // Set up filter clearing
    setupFilterClearing();
    
    // Set up dropdown functionality
    setupDropdowns();
});

/**
 * Initializes all progress bars in the table
 */
function initializeProgressBars() {
    document.querySelectorAll('.progress-value').forEach(function(el) {
        if (!el.style.width) {
            const progressText = el.parentElement.nextElementSibling.textContent;
            const progress = parseFloat(progressText);
            if (!isNaN(progress)) {
                el.style.width = progress + '%';
            }
        }
    });
}

/**
 * Sets up the auto-refresh checkbox functionality
 */
function setupAutoRefresh() {
    const autoRefreshCheckbox = document.getElementById('auto-refresh');
    if (autoRefreshCheckbox) {
        // Check local storage for previous setting
        const savedSetting = localStorage.getItem('autoRefreshEnabled');
        if (savedSetting !== null) {
            autoRefreshCheckbox.checked = savedSetting === 'true';
        }
        
        // Save setting when changed
        autoRefreshCheckbox.addEventListener('change', function() {
            localStorage.setItem('autoRefreshEnabled', this.checked);
        });
    }
}

/**
 * Determines if auto-refresh is enabled
 * This function is called by HTMX for the auto-refresh trigger
 */
function autoRefreshEnabled() {
    const checkbox = document.getElementById('auto-refresh');
    return checkbox ? checkbox.checked : false;
}

/**
 * Sets up column sorting functionality
 */
function setupSorting() {
    const sortableHeaders = document.querySelectorAll('th.sortable');
    const sortByInput = document.querySelector('input[name="sort-by"]');
    const sortDirInput = document.querySelector('input[name="sort-dir"]');
    
    if (!sortableHeaders.length || !sortByInput || !sortDirInput) return;
    
    // Update header styling based on current sorting
    updateSortHeaderStyles(sortByInput.value, sortDirInput.value);
    
    // Add click handlers for sortable headers
    sortableHeaders.forEach(header => {
        header.addEventListener('click', function() {
            const column = this.dataset.column;
            
            if (sortByInput.value === column) {
                // Toggle direction if already sorting by this column
                sortDirInput.value = sortDirInput.value === 'asc' ? 'desc' : 'asc';
            } else {
                // Default to ascending for new column
                sortByInput.value = column;
                sortDirInput.value = 'asc';
            }
            
            // Update header styles
            updateSortHeaderStyles(sortByInput.value, sortDirInput.value);
            
            // Trigger refresh
            document.body.dispatchEvent(new Event('refreshTable'));
        });
    });
}

/**
 * Updates the styling of sortable headers based on the current sort
 */
function updateSortHeaderStyles(sortBy, sortDir) {
    // Remove all sort classes first
    document.querySelectorAll('th.sortable').forEach(th => {
        th.classList.remove('sort-asc', 'sort-desc');
    });
    
    // Add the appropriate class to the current sort column
    if (sortBy) {
        const header = document.querySelector(`th[data-column="${sortBy}"]`);
        if (header) {
            header.classList.add(sortDir === 'desc' ? 'sort-desc' : 'sort-asc');
        }
    }
}

/**
 * Sets up filter clearing functionality
 */
function setupFilterClearing() {
    const clearButton = document.querySelector('.clear-filters');
    if (!clearButton) return;
    
    clearButton.addEventListener('click', function() {
        // Reset filter inputs
        document.querySelectorAll('select[name^="filter-"], input[name^="filter-"]').forEach(filter => {
            if (filter.tagName === 'SELECT') {
                filter.selectedIndex = 0;
            } else {
                filter.value = '';
            }
        });
    });
}

/**
 * Formats a date object as a string
 */
function formatDate(date) {
    if (!date) return '-';
    
    const options = { 
        year: 'numeric', 
        month: 'short', 
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    };
    
    return new Date(date).toLocaleDateString(undefined, options);
}

/**
 * Formats a duration in milliseconds to a human-readable string
 */
function formatDuration(ms) {
    if (!ms) return '-';
    
    const seconds = Math.floor((ms / 1000) % 60);
    const minutes = Math.floor((ms / (1000 * 60)) % 60);
    const hours = Math.floor(ms / (1000 * 60 * 60));
    
    return `${hours}h ${minutes}m ${seconds}s`;
}

/**
 * Sets up dropdown functionality for touch devices and better accessibility
 */
function setupDropdowns() {
    const dropdownButtons = document.querySelectorAll('.dropdown > button');
    
    dropdownButtons.forEach(button => {
        button.addEventListener('click', function(e) {
            e.preventDefault();
            e.stopPropagation();
            
            // Toggle the dropdown's content visibility
            const dropdownContent = this.nextElementSibling;
            
            // If this is the export dropdown, update the export URLs with current filter parameters
            if (this.classList.contains('btn-export')) {
                updateExportURLs();
            }
            
            // Close all other open dropdowns first
            document.querySelectorAll('.dropdown-content.show').forEach(content => {
                if (content !== dropdownContent) {
                    content.classList.remove('show');
                }
            });
            
            // Toggle this dropdown's visibility
            dropdownContent.classList.toggle('show');
        });
    });
    
    // Close dropdowns when clicking outside
    document.addEventListener('click', function(e) {
        if (!e.target.closest('.dropdown')) {
            document.querySelectorAll('.dropdown-content.show').forEach(content => {
                content.classList.remove('show');
            });
        }
    });
}

/**
 * Updates export URLs with current filter parameters
 */
function updateExportURLs() {
    // Get all current filter values
    const statusFilter = document.querySelector('[name="filter-status"]')?.value || '';
    const repoFilter = document.querySelector('[name="filter-repo"]')?.value || '';
    const timeRangeFilter = document.querySelector('[name="filter-timerange"]')?.value || '';
    const sortBy = document.querySelector('[name="sort-by"]')?.value || '';
    const sortDir = document.querySelector('[name="sort-dir"]')?.value || '';
    
    // Build query string with parameters
    const params = new URLSearchParams();
    if (statusFilter) params.append('filter-status', statusFilter);
    if (repoFilter) params.append('filter-repo', repoFilter);
    if (timeRangeFilter) params.append('filter-timerange', timeRangeFilter);
    if (sortBy) params.append('sort-by', sortBy);
    if (sortDir) params.append('sort-dir', sortDir);
    
    // Add format parameter for each export type
    const csvParams = new URLSearchParams(params);
    csvParams.append('format', 'csv');
    
    const jsonParams = new URLSearchParams(params);
    jsonParams.append('format', 'json');
    
    // Update export links
    const csvLink = document.querySelector('.dropdown-item[download="migrations.csv"]');
    const jsonLink = document.querySelector('.dropdown-item[download="migrations.json"]');
    
    if (csvLink) csvLink.href = `/dashboard/export?${csvParams.toString()}`;
    if (jsonLink) jsonLink.href = `/dashboard/export?${jsonParams.toString()}`;
} 
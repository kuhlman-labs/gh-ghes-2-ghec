// Set progress bar widths based on data-progress attributes
document.addEventListener('DOMContentLoaded', function() {
    // Initialize progress bars
    updateProgressBars();
    
    // Add event listener for HTMX after-swap to handle dynamically loaded content
    document.body.addEventListener('htmx:afterSwap', function() {
        updateProgressBars();
    });

    // Set up the auto-refresh toggle
    const autoRefreshCheckbox = document.getElementById('auto-refresh');
    if (autoRefreshCheckbox) {
        autoRefreshCheckbox.addEventListener('change', function() {
            // Force a refresh immediately when turning auto-refresh back on
            if (this.checked) {
                const migrationsTable = document.getElementById('migrations-table');
                if (migrationsTable) {
                    htmx.trigger(migrationsTable, 'refreshTable');
                }
            }
        });
    }
});

// Function used by HTMX to check if auto-refresh is enabled
function autoRefreshEnabled() {
    const checkbox = document.getElementById('auto-refresh');
    return checkbox && checkbox.checked;
}

function updateProgressBars() {
    // Get all progress bar elements with data-progress attribute
    const progressBars = document.querySelectorAll('.progress-value[data-progress], .stage-progress-value[data-progress]');
    
    // Set the width style for each progress bar based on its data-progress value
    progressBars.forEach(function(bar) {
        const progress = bar.getAttribute('data-progress');
        if (progress !== null) {
            bar.style.width = progress + '%';
        }
    });
} 
// Set progress bar widths based on data-progress attributes
document.addEventListener('DOMContentLoaded', function() {
    // Initialize progress bars
    updateProgressBars();
    
    // Add event listener for HTMX after-swap to handle dynamically loaded content
    document.body.addEventListener('htmx:afterSwap', function() {
        updateProgressBars();
    });
});

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
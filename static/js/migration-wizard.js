/**
 * Migration Wizard JavaScript
 * Implements wizard functionality, validation, auto-save, and enhanced UX
 */

class MigrationWizard {
    constructor() {
        this.currentStep = 1;
        this.maxSteps = 6;
        this.selectedTemplate = null;
        this.wizardData = {};
        this.autoSaveTimer = null;
        this.hasDraft = false;
        this.isNavigating = false; // Prevent rapid navigation calls
        this.validationRules = this.initValidationRules();
        this.connectionsTested = {
            source: false,
            target: false
        };
        this.allRepositories = []; // Store all loaded repositories for client-side filtering
        this.repositorySearchHandler = null; // Store the search handler for cleanup
        
        this.init();
    }

    init() {
        this.bindEvents();
        this.initializeTemplateSelection();
        this.loadDraftData();
        this.setupAutoSave();
        this.initializeValidation();
    }

    initValidationRules() {
        return {
            url: {
                pattern: /^https?:\/\/[^\s/$.?#].[^\s]*$/i,
                message: 'Please enter a valid URL starting with http:// or https://'
            },
            token: {
                pattern: /^.{30,}$/,
                message: 'Please enter a valid GitHub token (minimum 30 characters)'
            },
            required: {
                test: (value) => value && value.trim().length > 0,
                message: 'This field is required'
            },
            repositories: {
                test: (value) => {
                    if (!value || !value.trim()) return false;
                    const repos = value.trim().split('\n').filter(r => r.trim());
                    return repos.length > 0 && repos.every(r => /^[a-zA-Z0-9._-]+$/.test(r.trim()));
                },
                message: 'Please enter valid repository names, one per line'
            },
            duration: {
                pattern: /^\d+[hmd]$/,
                message: 'Please enter a valid duration (e.g., 24h, 7d, 30m)'
            }

        };
    }

    bindEvents() {
        // Template selection
        document.addEventListener('click', (e) => {
            if (e.target.closest('.template-card')) {
                this.selectTemplate(e.target.closest('.template-card'));
            }
        });

        // Step navigation
        document.getElementById('next-btn')?.addEventListener('click', () => this.nextStep());
        document.getElementById('prev-btn')?.addEventListener('click', () => this.previousStep());

        // Form submission
        const form = document.getElementById('migration-wizard-form');
        if (form) {
            form.addEventListener('submit', (e) => this.handleSubmit(e));
        }

        // Schedule type toggle
        document.querySelectorAll('input[name="schedule_type"]').forEach(radio => {
            radio.addEventListener('change', () => this.toggleScheduleOptions());
        });

        // Tab switching
        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.addEventListener('click', () => this.switchTab(btn));
        });

        // Form field changes for auto-save
        document.addEventListener('input', (e) => {
            if (e.target.form?.id === 'migration-wizard-form') {
                this.scheduleAutoSave();
                this.validateField(e.target);
                
                // Reset connection test status when credentials change
                this.resetConnectionTestOnFieldChange(e.target);
            }
        });

        // Step indicator clicks
        document.querySelectorAll('.step-indicator').forEach((indicator, index) => {
            indicator.addEventListener('click', () => {
                const stepNum = index + 1;
                if (stepNum <= this.currentStep || this.canNavigateToStep(stepNum)) {
                    this.goToStep(stepNum);
                }
            });
        });

        // Connection testing
        window.testConnection = (type) => this.testConnection(type);
        window.togglePassword = (fieldId) => this.togglePassword(fieldId);
        window.startWizard = () => this.startWizard();
        window.saveDraft = () => this.saveDraft();
        window.loadRepositories = () => this.loadRepositories();
        window.handleFileUpload = (event) => this.handleFileUpload(event);
        window.nextStep = () => this.nextStep();
        window.previousStep = () => this.previousStep();
        window.clearDraft = () => this.clearDraft();
        window.handleCancel = () => this.handleCancel();
        window.debugWizardState = () => this.debugWizardState();
    }

    initializeTemplateSelection() {
        // Set default template selection
        const defaultTemplate = document.querySelector('.template-card[data-template="quick-start"]');
        if (defaultTemplate) {
            this.selectTemplate(defaultTemplate);
        }
    }

    selectTemplate(templateCard) {
        // Remove previous selection
        document.querySelectorAll('.template-card').forEach(card => {
            card.classList.remove('selected');
        });

        // Add selection to clicked card
        templateCard.classList.add('selected');
        
        // Store selected template
        this.selectedTemplate = templateCard.dataset.template;
        
        // Apply template defaults
        this.applyTemplateDefaults(this.selectedTemplate);
    }

    applyTemplateDefaults(templateType) {
        const templates = {
            'quick-start': {
                use_ghos: true,
                delete_if_exists: false,
                max_duration: '',
                schedule_type: 'immediate'
            },
            'scheduled': {
                use_ghos: true,
                delete_if_exists: false,
                max_duration: '24h',
                schedule_type: 'scheduled'
            },
            'bulk': {
                use_ghos: true,
                delete_if_exists: true,
                max_duration: '48h',
                schedule_type: 'immediate'
            },
            'custom': {
                use_ghos: true,
                delete_if_exists: false,
                max_duration: '',
                schedule_type: 'immediate'
            }
        };

        const defaults = templates[templateType] || templates['custom'];
        this.wizardData.template = templateType;
        this.wizardData.defaults = defaults;
    }

    startWizard() {
        if (!this.selectedTemplate) {
            this.showError('Please select a migration template');
            return;
        }

        // Hide template selection, show wizard form
        document.getElementById('template-selection').style.display = 'none';
        document.getElementById('migration-wizard-form').style.display = 'block';

        // Set template type
        document.getElementById('template-type').value = this.selectedTemplate;

        // If we have draft data, restore it, otherwise apply defaults
        if (this.hasDraft && this.wizardData) {
            this.restoreFormData();
            this.goToStep(this.currentStep);
        } else {
            this.applyDefaultsToForm();
            this.goToStep(1);
        }
        
        this.updateProgress();
    }

    applyDefaultsToForm() {
        if (!this.wizardData.defaults) return;

        const defaults = this.wizardData.defaults;
        
        Object.entries(defaults).forEach(([key, value]) => {
            const field = document.getElementById(key);
            if (field) {
                if (field.type === 'checkbox') {
                    field.checked = value;
                } else if (field.type === 'radio') {
                    const radio = document.querySelector(`input[name="${key}"][value="${value}"]`);
                    if (radio) radio.checked = true;
                } else {
                    field.value = value;
                }
            }
        });

        // Trigger schedule type change if needed
        if (defaults.schedule_type) {
            this.toggleScheduleOptions();
        }
    }

    nextStep() {
        if (this.currentStep >= this.maxSteps) return;

        // Prevent rapid multiple calls
        if (this.isNavigating) {
            console.log('Navigation in progress, skipping rapid call');
            return;
        }
        this.isNavigating = true;

        console.log(`NextStep called: current step is ${this.currentStep}`);

        // Validate current step
        if (!this.validateCurrentStep()) {
            // Provide more specific error messages based on the step
            let errorMessage = 'Please fix the errors before continuing';
            
            if (this.currentStep === 1) {
                if (!this.connectionsTested.source) {
                    errorMessage = 'Please test the source connection before proceeding to the next step';
                } else {
                    errorMessage = 'Please check that all source configuration fields are valid';
                }
            } else if (this.currentStep === 2) {
                if (!this.connectionsTested.target) {
                    errorMessage = 'Please test the target connection before proceeding to the next step';
                } else {
                    errorMessage = 'Please check that all target configuration fields are valid';
                }
            } else if (this.currentStep === 3) {
                errorMessage = 'Please select at least one repository to migrate';
            }
            
            this.showError(errorMessage);
            this.isNavigating = false;
            return;
        }

        // Save current step data
        this.saveStepData();

        // Move to next step
        const nextStepNumber = this.currentStep + 1;
        console.log(`NextStep: moving from ${this.currentStep} to ${nextStepNumber}`);
        
        this.goToStep(nextStepNumber);
        
        // Allow navigation again after a short delay
        setTimeout(() => {
            this.isNavigating = false;
        }, 100);
    }

    previousStep() {
        if (this.currentStep <= 1) return;

        // Prevent rapid multiple calls
        if (this.isNavigating) {
            console.log('Navigation in progress, skipping rapid call');
            return;
        }
        this.isNavigating = true;

        const prevStepNumber = this.currentStep - 1;
        console.log(`PreviousStep: moving from ${this.currentStep} to ${prevStepNumber}`);
        
        this.goToStep(prevStepNumber);
        
        // Allow navigation again after a short delay
        setTimeout(() => {
            this.isNavigating = false;
        }, 100);
    }

    hideAllSteps() {
        console.log('Hiding all steps...');
        document.querySelectorAll('#migration-wizard-form .wizard-step').forEach((step, index) => {
            step.classList.remove('active');
            step.style.display = 'none';
            step.style.visibility = 'hidden';
            step.style.height = '0';
            step.style.overflow = 'hidden';
            console.log(`Step ${index + 1} hidden:`, step.id);
        });
        
        // Debug: check which steps are still visible
        this.debugVisibleSteps();
    }

    debugVisibleSteps() {
        const visibleSteps = [];
        document.querySelectorAll('#migration-wizard-form .wizard-step').forEach((step, index) => {
            const isVisible = step.style.display !== 'none' && 
                             step.style.visibility !== 'hidden' && 
                             step.classList.contains('active');
            if (isVisible) {
                visibleSteps.push(`Step ${index + 1} (${step.id})`);
            }
        });
        
        if (visibleSteps.length > 1) {
            console.warn('Multiple steps visible:', visibleSteps);
        } else if (visibleSteps.length === 1) {
            console.log('Currently visible:', visibleSteps[0]);
        } else {
            console.log('No steps visible');
        }
    }

    // Add comprehensive debug function
    debugWizardState() {
        console.log('=== WIZARD DEBUG STATE ===');
        console.log('Current step:', this.currentStep);
        
        document.querySelectorAll('#migration-wizard-form .wizard-step').forEach((step, index) => {
            const stepNum = index + 1;
            const computedStyle = window.getComputedStyle(step);
            const stepInfo = {
                id: step.id,
                hasActiveClass: step.classList.contains('active'),
                display: step.style.display,
                visibility: step.style.visibility,
                height: step.style.height,
                overflow: step.style.overflow,
                computedDisplay: computedStyle.display,
                computedVisibility: computedStyle.visibility,
                computedHeight: computedStyle.height,
                computedOpacity: computedStyle.opacity,
                offsetHeight: step.offsetHeight,
                offsetWidth: step.offsetWidth
            };
            
            console.log(`Step ${stepNum}:`, stepInfo);
            
            if (stepInfo.offsetHeight > 0 || stepInfo.computedDisplay !== 'none') {
                console.warn(`⚠️ Step ${stepNum} might be visible!`);
            }
        });
        
        console.log('=== END DEBUG STATE ===');
    }

    goToStep(stepNumber) {
        if (stepNumber < 1 || stepNumber > this.maxSteps) {
            console.warn(`Invalid step number: ${stepNumber}`);
            return;
        }

        console.log(`Going to step ${stepNumber} from step ${this.currentStep}`);
        
        // Prevent multiple rapid calls to the same step
        if (this.currentStep === stepNumber) {
            console.log(`Already on step ${stepNumber}, skipping`);
            return;
        }
        
        // Update current step immediately
        const previousStep = this.currentStep;
        this.currentStep = stepNumber;

        // Force immediate style reset for all steps
        document.querySelectorAll('#migration-wizard-form .wizard-step').forEach((step, index) => {
            const stepNum = index + 1;
            const isTargetStep = stepNum === stepNumber;
            
            // Remove all classes first
            step.classList.remove('active');
            
            // Force hide all steps immediately
            step.style.display = 'none';
            step.style.visibility = 'hidden';
            step.style.height = '0';
            step.style.overflow = 'hidden';
            step.style.opacity = '0';
            
            console.log(`Step ${stepNum} (${step.id}) hidden`);
        });

        // Force a reflow to ensure styles are applied
        document.getElementById('migration-wizard-form').offsetHeight;

        // Now show only the target step
        const targetStepEl = document.getElementById(`step-${stepNumber}`);
        if (targetStepEl) {
            targetStepEl.style.display = 'block';
            targetStepEl.style.visibility = 'visible';
            targetStepEl.style.height = 'auto';
            targetStepEl.style.overflow = 'visible';
            targetStepEl.style.opacity = '1';
            targetStepEl.classList.add('active');
            
            console.log(`Step ${stepNumber} (${targetStepEl.id}) activated and shown`);
        } else {
            console.error(`Step element not found: step-${stepNumber}`);
        }

        // Debug check immediately
        setTimeout(() => this.debugVisibleSteps(), 10);

        // Update navigation buttons
        this.updateNavigationButtons();

        // Update progress indicator
        this.updateProgress();

        // Update review section if on final step
        if (stepNumber === this.maxSteps) {
            this.updateReviewSection();
        }

        // Show connection test if credentials are filled
        this.checkConnectionTestVisibility();
        
        console.log(`Step transition complete: ${previousStep} -> ${stepNumber}`);
    }

    updateNavigationButtons() {
        const prevBtn = document.getElementById('prev-btn');
        const nextBtn = document.getElementById('next-btn');
        const submitBtn = document.getElementById('submit-btn');

        // Previous button
        if (prevBtn) {
            prevBtn.style.display = this.currentStep > 1 ? 'inline-flex' : 'none';
        }

        // Next/Submit buttons
        if (this.currentStep === this.maxSteps) {
            if (nextBtn) nextBtn.style.display = 'none';
            if (submitBtn) submitBtn.style.display = 'inline-flex';
        } else {
            if (nextBtn) nextBtn.style.display = 'inline-flex';
            if (submitBtn) submitBtn.style.display = 'none';
        }
    }

    updateProgress() {
        // Update step indicators
        document.querySelectorAll('.step-indicator').forEach((indicator, index) => {
            const stepNum = index + 1;
            indicator.classList.remove('active', 'completed');
            
            if (stepNum === this.currentStep) {
                indicator.classList.add('active');
            } else if (stepNum < this.currentStep) {
                indicator.classList.add('completed');
            }
        });

        // Update progress line
        const progressLine = document.querySelector('.progress-line::after, .progress-line');
        if (progressLine) {
            const progressPercent = ((this.currentStep - 1) / (this.maxSteps - 1)) * 100;
            progressLine.style.setProperty('--progress-width', `${progressPercent}%`);
            
            // Update CSS custom property
            document.documentElement.style.setProperty('--wizard-progress', `${progressPercent}%`);
        }
    }

    validateCurrentStep() {
        const currentStepEl = document.getElementById(`step-${this.currentStep}`);
        if (!currentStepEl) return true;

        const fields = currentStepEl.querySelectorAll('input[required], textarea[required], select[required]');
        let isValid = true;

        fields.forEach(field => {
            if (!this.validateField(field)) {
                isValid = false;
            }
        });

        // Step-specific validation
        switch (this.currentStep) {
            case 1: // Source configuration
                if (!this.validateConnectionTested('source')) {
                    isValid = false;
                }
                break;
            case 2: // Target configuration
                if (!this.validateConnectionTested('target')) {
                    isValid = false;
                }
                break;
            case 3: // Repository selection
                if (!this.validateRepositories()) {
                    isValid = false;
                }
                break;
            case 6: // Review step
                if (!document.getElementById('confirm_migration')?.checked) {
                    this.showValidationError('confirm_migration', 'You must confirm the migration configuration');
                    isValid = false;
                }
                break;
        }

        return isValid;
    }

    validateField(field) {
        if (!field) return true;

        const validationType = field.dataset.validation;
        const value = field.value;
        let isValid = true;
        let message = '';

        // Debug logging for token validation
        if (field.id.includes('token')) {
            console.log('Validating token field:', {
                fieldId: field.id,
                hasValue: !!value,
                valueLength: value ? value.length : 0,
                validationType: validationType,
                isRequired: field.required
            });
        }

        // Clear previous error
        this.clearValidationError(field.id);

        // Required validation
        if (field.required && (!value || !value.trim())) {
            isValid = false;
            message = 'This field is required';
        }

        // Type-specific validation
        if (isValid && validationType && value) {
            const rule = this.validationRules[validationType];
            if (rule) {
                if (rule.pattern && !rule.pattern.test(value)) {
                    isValid = false;
                    message = rule.message;
                    console.log('Token validation failed:', {
                        pattern: rule.pattern,
                        value: value.substring(0, 8) + '...',
                        testResult: rule.pattern.test(value)
                    });
                } else if (rule.test && !rule.test(value)) {
                    isValid = false;
                    message = rule.message;
                }
            }
        }

        // Show validation error
        if (!isValid) {
            this.showValidationError(field.id, message);
        }

        // Update field styling
        field.classList.toggle('invalid', !isValid);

        return isValid;
    }

    validateRepositories() {
        const activeTab = document.querySelector('.tab-content.active');
        console.log('Validating repositories - active tab:', activeTab?.id);
        
        if (!activeTab) {
            console.log('No active tab found');
            return false;
        }

        if (activeTab.id === 'manual-tab') {
            const textarea = document.getElementById('repositories');
            console.log('Manual tab - textarea value:', textarea?.value);
            return this.validateField(textarea);
        } else if (activeTab.id === 'browse-tab' || activeTab.id === 'bulk-tab') {
            const selectedRepos = this.getSelectedRepositories();
            console.log('Browse/Bulk tab - selected repos:', selectedRepos);
            console.log('Selected repos count:', selectedRepos.length);
            
            if (selectedRepos.length === 0) {
                this.showError('Please select at least one repository');
                return false;
            }
            
            // Also update the hidden repositories textarea with selected repos
            const textarea = document.getElementById('repositories');
            if (textarea) {
                textarea.value = selectedRepos.join('\n');
                console.log('Updated repositories textarea with:', textarea.value);
            }
        }

        return true;
    }

    validateConnectionTested(connectionType) {
        if (!this.connectionsTested[connectionType]) {
            const statusEl = document.getElementById(`${connectionType}-connection-status`);
            if (statusEl) {
                statusEl.textContent = '⚠ Please test the connection before proceeding';
                statusEl.className = 'connection-status warning';
            }
            this.showError(`Please test the ${connectionType} connection before proceeding to the next step`);
            return false;
        }
        return true;
    }

    resetConnectionTestOnFieldChange(field) {
        // Reset connection test status when relevant fields change
        const fieldId = field.id;
        
        // Source connection fields
        if (['ghes_base_url', 'ghes_token', 'source_org'].includes(fieldId)) {
            this.connectionsTested.source = false;
            const statusEl = document.getElementById('source-connection-status');
            if (statusEl) {
                statusEl.textContent = '';
                statusEl.className = 'connection-status';
            }
        }
        
        // Target connection fields
        if (['gh_cloud_token', 'target_org'].includes(fieldId)) {
            this.connectionsTested.target = false;
            const statusEl = document.getElementById('target-connection-status');
            if (statusEl) {
                statusEl.textContent = '';
                statusEl.className = 'connection-status';
            }
        }
    }

    showValidationError(fieldId, message) {
        const errorEl = document.getElementById(`${fieldId}-error`);
        if (errorEl) {
            errorEl.textContent = message;
            errorEl.classList.add('show');
        }
    }

    clearValidationError(fieldId) {
        const errorEl = document.getElementById(`${fieldId}-error`);
        if (errorEl) {
            errorEl.classList.remove('show');
        }
    }

    saveStepData() {
        const currentStepEl = document.getElementById(`step-${this.currentStep}`);
        if (!currentStepEl) return;

        // Save form data for current step
        const formData = new FormData(document.getElementById('migration-wizard-form'));
        const stepData = {};

        // Convert FormData to object for current step fields
        const fields = currentStepEl.querySelectorAll('input, textarea, select');
        fields.forEach(field => {
            if (field.type === 'checkbox') {
                stepData[field.name] = field.checked;
            } else if (field.type === 'radio') {
                if (field.checked) {
                    stepData[field.name] = field.value;
                }
            } else {
                stepData[field.name] = field.value;
            }
        });

        this.wizardData[`step${this.currentStep}`] = stepData;
    }

    scheduleAutoSave() {
        if (this.autoSaveTimer) {
            clearTimeout(this.autoSaveTimer);
        }

        this.autoSaveTimer = setTimeout(() => {
            this.saveDraft();
        }, 2000); // Auto-save after 2 seconds of inactivity
    }

    saveDraft() {
        this.saveStepData();
        
        // Prepare draft data
        const draftData = {
            currentStep: this.currentStep,
            selectedTemplate: this.selectedTemplate,
            wizardData: this.wizardData,
            timestamp: new Date().toISOString()
        };

        // Save to localStorage as backup
        localStorage.setItem('migration-wizard-draft', JSON.stringify(draftData));
        
        // Also save to server
        const formData = new FormData();
        formData.append('draft_data', JSON.stringify(draftData));
        formData.append('draft_name', 'auto-save-' + Date.now());

        fetch('/dashboard/wizard/save-draft', {
            method: 'POST',
            body: formData
        })
        .then(response => response.json())
        .then(data => {
            if (data.success) {
                this.showAutoSaveIndicator();
            }
        })
        .catch(error => {
            console.error('Draft save error:', error);
            // Still show indicator since localStorage save succeeded
            this.showAutoSaveIndicator();
        });
    }

    loadDraftData() {
        const draftData = localStorage.getItem('migration-wizard-draft');
        if (!draftData) return;

        try {
            const draft = JSON.parse(draftData);
            
            // Check if draft is recent (within 24 hours)
            const draftAge = Date.now() - new Date(draft.timestamp).getTime();
            if (draftAge > 24 * 60 * 60 * 1000) {
                localStorage.removeItem('migration-wizard-draft');
                return;
            }

            // Store draft data but don't auto-continue to wizard
            this.selectedTemplate = draft.selectedTemplate;
            this.wizardData = draft.wizardData;
            this.currentStep = draft.currentStep || 1;
            this.hasDraft = true;
            
            // Select template to show it's selected
            if (this.selectedTemplate) {
                const templateCard = document.querySelector(`[data-template="${this.selectedTemplate}"]`);
                if (templateCard) {
                    this.selectTemplate(templateCard);
                }
            }
            
            // Show draft indicator with continue option
            this.showDraftIndicator();
        } catch (error) {
            console.error('Error loading draft data:', error);
            localStorage.removeItem('migration-wizard-draft');
        }
    }

    restoreFormData() {
        Object.values(this.wizardData).forEach(stepData => {
            if (typeof stepData === 'object') {
                Object.entries(stepData).forEach(([name, value]) => {
                    const field = document.querySelector(`[name="${name}"]`);
                    if (field) {
                        if (field.type === 'checkbox') {
                            field.checked = value;
                        } else if (field.type === 'radio') {
                            if (field.value === value) {
                                field.checked = true;
                            }
                        } else {
                            field.value = value;
                        }
                    }
                });
            }
        });
    }

    showAutoSaveIndicator() {
        const indicator = document.getElementById('auto-save-indicator');
        if (indicator) {
            indicator.style.display = 'flex';
            setTimeout(() => {
                indicator.style.display = 'none';
            }, 2000);
        }
    }

    showDraftIndicator() {
        const draftIndicator = document.getElementById('draft-indicator');
        if (draftIndicator) {
            draftIndicator.style.display = 'flex';
        }
    }

    clearDraft() {
        // Clear localStorage
        localStorage.removeItem('migration-wizard-draft');
        
        // Reset wizard state
        this.selectedTemplate = null;
        this.wizardData = {};
        this.currentStep = 1;
        this.hasDraft = false;
        
        // Reset form
        const form = document.getElementById('migration-wizard-form');
        if (form) {
            form.reset();
        }
        
        // Reset template selection
        document.querySelectorAll('.template-card').forEach(card => {
            card.classList.remove('selected');
        });
        
        // Hide wizard form and show template selection
        document.getElementById('template-selection').style.display = 'block';
        document.getElementById('migration-wizard-form').style.display = 'none';
        
        // Hide draft indicator
        const draftIndicator = document.getElementById('draft-indicator');
        if (draftIndicator) {
            draftIndicator.style.display = 'none';
        }
        
        // Show success message
        this.showMessage('Draft cleared. Starting fresh!', 'success');
    }

    handleCancel() {
        const wizardForm = document.getElementById('migration-wizard-form');
        const templateSelection = document.getElementById('template-selection');
        
        // If we're in the wizard form, go back to template selection
        if (wizardForm.style.display !== 'none') {
            wizardForm.style.display = 'none';
            templateSelection.style.display = 'block';
        } else {
            // If we're in template selection, go back to dashboard
            window.location.href = '/dashboard';
        }
    }

    showMessage(message, type = 'info') {
        // Create temporary message element
        const messageEl = document.createElement('div');
        messageEl.className = `wizard-message wizard-message-${type}`;
        messageEl.innerHTML = `
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M20 6L9 17l-5-5"/>
            </svg>
            ${message}
        `;
        
        const container = document.querySelector('.wizard-container');
        if (container) {
            container.insertBefore(messageEl, container.firstChild);
            
            // Auto-remove after 3 seconds
            setTimeout(() => {
                messageEl.remove();
            }, 3000);
        }
    }

    toggleScheduleOptions() {
        const scheduledRadio = document.getElementById('scheduled');
        const scheduledOptions = document.getElementById('scheduled-options');
        
        if (scheduledOptions) {
            scheduledOptions.style.display = scheduledRadio?.checked ? 'block' : 'none';
        }
    }

    switchTab(tabBtn) {
        const tabName = tabBtn.dataset.tab;
        
        // Update tab buttons
        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.classList.remove('active');
        });
        tabBtn.classList.add('active');

        // Update tab content
        document.querySelectorAll('.tab-content').forEach(content => {
            content.classList.remove('active');
        });
        document.getElementById(`${tabName}-tab`)?.classList.add('active');
    }

    testConnection(type) {
        const button = document.querySelector(`#${type}-connection-test button`);
        const status = document.getElementById(`${type}-connection-status`);
        
        if (!button || !status) return;

        // Get connection details
        let url, token, org;
        if (type === 'source') {
            const urlField = document.getElementById('ghes_base_url');
            const tokenField = document.getElementById('ghes_token');
            const orgField = document.getElementById('source_org');
            
            url = urlField?.value || '';
            token = tokenField?.value || '';
            org = orgField?.value || '';
            
            console.log('Source connection test - field values:', {
                urlField: urlField ? 'found' : 'NOT FOUND',
                tokenField: tokenField ? 'found' : 'NOT FOUND', 
                orgField: orgField ? 'found' : 'NOT FOUND',
                url: url,
                token: token ? `${token.substring(0, 8)}...` : 'EMPTY',
                org: org
            });
        } else {
            const tokenField = document.getElementById('gh_cloud_token');
            const orgField = document.getElementById('target_org');
            
            url = 'https://api.github.com';
            token = tokenField?.value || '';
            org = orgField?.value || '';
            
            console.log('Target connection test - field values:', {
                tokenField: tokenField ? 'found' : 'NOT FOUND',
                orgField: orgField ? 'found' : 'NOT FOUND',
                url: url,
                token: token ? `${token.substring(0, 8)}...` : 'EMPTY',
                org: org
            });
        }

        if (!url || !token || !org) {
            status.textContent = 'Please fill in all required fields first';
            status.className = 'connection-status error';
            return;
        }

        // Show testing state
        button.classList.add('loading');
        status.textContent = 'Testing connection...';
        status.className = 'connection-status testing';

        // Make actual API call to test connection
        const formData = new FormData();
        formData.append('type', type);
        formData.append('token', token);
        formData.append('org', org);
        formData.append('base_url', url);
        
        // Debug logging
        console.log('Connection test data:', {
            type: type,
            token: token ? `${token.substring(0, 8)}...` : 'EMPTY',
            org: org,
            base_url: url
        });

        fetch('/dashboard/wizard/test-connection', {
            method: 'POST',
            body: formData
        })
        .then(response => response.json())
        .then(data => {
            button.classList.remove('loading');
            
            if (data.success) {
                status.textContent = '✓ ' + data.message;
                status.className = 'connection-status success';
                // Mark this connection type as successfully tested
                this.connectionsTested[type] = true;
            } else {
                status.textContent = '✗ ' + data.message;
                status.className = 'connection-status error';
                // Mark this connection type as not tested
                this.connectionsTested[type] = false;
            }
        })
        .catch(error => {
            button.classList.remove('loading');
            status.textContent = '✗ Connection test failed';
            status.className = 'connection-status error';
            // Mark this connection type as not tested
            this.connectionsTested[type] = false;
            console.error('Connection test error:', error);
        });
    }

    togglePassword(fieldId) {
        const field = document.getElementById(fieldId);
        const eyeIcon = document.getElementById(`${fieldId}-eye`);
        
        if (!field || !eyeIcon) return;

        if (field.type === 'password') {
            field.type = 'text';
            eyeIcon.innerHTML = `
                <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
                <line x1="1" y1="1" x2="23" y2="23"/>
            `;
        } else {
            field.type = 'password';
            eyeIcon.innerHTML = `
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
                <circle cx="12" cy="12" r="3"/>
            `;
        }
    }

    checkConnectionTestVisibility() {
        // Show connection test when credentials are filled
        if (this.currentStep === 1) {
            const url = document.getElementById('ghes_base_url')?.value;
            const token = document.getElementById('ghes_token')?.value;
            const org = document.getElementById('source_org')?.value;
            
            const testDiv = document.getElementById('source-connection-test');
            if (testDiv) {
                testDiv.style.display = (url && token && org) ? 'block' : 'none';
            }
        } else if (this.currentStep === 2) {
            const token = document.getElementById('gh_cloud_token')?.value;
            const org = document.getElementById('target_org')?.value;
            
            const testDiv = document.getElementById('target-connection-test');
            if (testDiv) {
                testDiv.style.display = (token && org) ? 'block' : 'none';
            }
        }
    }

    loadRepositories() {
        const button = event.target;
        const repoList = document.getElementById('repo-list');
        const searchInput = document.getElementById('repo-search');
        
        if (!repoList) return;

        // Get connection details from step 1
        const token = document.getElementById('ghes_token')?.value;
        const org = document.getElementById('source_org')?.value;
        const baseURL = document.getElementById('ghes_base_url')?.value;

        if (!token || !org || !baseURL) {
            repoList.innerHTML = '<div class="error-state"><p>Please complete source configuration first</p></div>';
            return;
        }

        button.classList.add('loading');
        
        // Make actual API call to load repositories (load all repositories, no server-side filtering)
        const formData = new FormData();
        formData.append('token', token);
        formData.append('org', org);
        formData.append('base_url', baseURL);
        formData.append('search', ''); // Always load all repositories

        fetch('/dashboard/wizard/load-repositories', {
            method: 'POST',
            body: formData
        })
        .then(response => response.json())
        .then(data => {
            button.classList.remove('loading');
            
            if (data.success && data.repositories) {
                const repos = data.repositories;
                
                if (repos.length === 0) {
                    repoList.innerHTML = '<div class="empty-state"><p>No repositories found in organization</p></div>';
                    return;
                }
                
                // Store all repositories for client-side filtering
                this.allRepositories = repos;
                
                // Render repositories
                this.renderRepositories(repos);
                
                // Enable search and set up real-time filtering
                this.enableRepositorySearch();
                this.setupRepositorySearch();
                
            } else {
                repoList.innerHTML = `<div class="error-state"><p>${data.message || 'Failed to load repositories'}</p></div>`;
            }
        })
        .catch(error => {
            button.classList.remove('loading');
            repoList.innerHTML = '<div class="error-state"><p>Failed to load repositories</p></div>';
            console.error('Load repositories error:', error);
        });
    }

    renderRepositories(repos) {
        const repoList = document.getElementById('repo-list');
        if (!repoList) return;

        if (repos.length === 0) {
            repoList.innerHTML = '<div class="empty-state"><p>No repositories match your search</p></div>';
            return;
        }

        // Get currently selected repositories to preserve checked state
        const currentlySelected = this.getSelectedRepositories();

        repoList.innerHTML = repos.map(repo => {
            const isSelected = currentlySelected.includes(repo.name);
            return `
                <div class="repo-item">
                    <input type="checkbox" id="repo-${repo.name}" value="${repo.name}" 
                           ${isSelected ? 'checked' : ''} 
                           onchange="window.migrationWizard.updateSelectedRepos()">
                    <div class="repo-item-info">
                        <div class="repo-item-name">
                            ${repo.name}
                            ${repo.private ? '<span class="repo-private">Private</span>' : ''}
                        </div>
                        <div class="repo-item-desc">${repo.description || 'No description'}</div>
                        <div class="repo-item-size">${this.formatSize(repo.size)}</div>
                    </div>
                </div>
            `;
        }).join('');
    }

    enableRepositorySearch() {
        const searchInput = document.getElementById('repo-search');
        if (searchInput) {
            searchInput.disabled = false;
            searchInput.placeholder = 'Search repositories by name or description...';
        }
    }

    setupRepositorySearch() {
        const searchInput = document.getElementById('repo-search');
        const clearBtn = document.querySelector('.search-clear-btn');
        if (!searchInput) return;

        // Remove any existing event listeners
        if (this.repositorySearchHandler) {
            searchInput.removeEventListener('input', this.repositorySearchHandler);
        }
        
        // Create a debounced search handler
        this.repositorySearchHandler = this.debounce((event) => {
            const searchQuery = event.target.value.toLowerCase().trim();
            this.filterRepositories(searchQuery);
            
            // Show/hide clear button
            if (clearBtn) {
                clearBtn.style.display = searchQuery ? 'flex' : 'none';
            }
        }, 300);

        // Add the event listener
        searchInput.addEventListener('input', this.repositorySearchHandler);
        
        // Handle clear button clicks
        if (clearBtn) {
            clearBtn.style.display = 'none'; // Initially hidden
        }
    }

    filterRepositories(searchQuery) {
        if (!this.allRepositories) return;

        let filteredRepos = this.allRepositories;

        if (searchQuery) {
            filteredRepos = this.allRepositories.filter(repo => {
                return repo.name.toLowerCase().includes(searchQuery) ||
                       (repo.description && repo.description.toLowerCase().includes(searchQuery));
            });
        }

        this.renderRepositories(filteredRepos);
    }

    debounce(func, wait) {
        let timeout;
        return function executedFunction(...args) {
            const later = () => {
                clearTimeout(timeout);
                func(...args);
            };
            clearTimeout(timeout);
            timeout = setTimeout(later, wait);
        };
    }

    formatSize(bytes) {
        if (bytes === 0) return '0 B';
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
    }

    updateSelectedRepos() {
        const selectedRepos = this.getSelectedRepositories();
        console.log('updateSelectedRepos called - selected repos:', selectedRepos);
        
        const countEl = document.getElementById('selected-count');
        const listEl = document.getElementById('selected-list');
        
        if (countEl) countEl.textContent = selectedRepos.length;
        
        if (listEl) {
            listEl.innerHTML = selectedRepos.map(repo => `
                <div class="selected-repo">
                    ${repo}
                    <button type="button" onclick="window.migrationWizard.removeSelectedRepo('${repo}')">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <line x1="18" y1="6" x2="6" y2="18"></line>
                            <line x1="6" y1="6" x2="18" y2="18"></line>
                        </svg>
                    </button>
                </div>
            `).join('');
        }

        // Always update repositories textarea with current selection for form submission
        const repositoriesTextarea = document.getElementById('repositories');
        if (repositoriesTextarea) {
            const currentValue = repositoriesTextarea.value;
            const newValue = selectedRepos.join('\n');
            repositoriesTextarea.value = newValue;
            console.log('Updated repositories textarea:', {
                currentValue: currentValue,
                newValue: newValue,
                selectedReposLength: selectedRepos.length
            });
        } else {
            console.error('repositories textarea not found');
        }
    }

    getSelectedRepositories() {
        const activeTab = document.querySelector('.tab-content.active');
        console.log('getSelectedRepositories - active tab:', activeTab?.id);
        
        if (!activeTab) {
            console.log('No active tab found in getSelectedRepositories');
            return [];
        }

        if (activeTab.id === 'manual-tab') {
            const textarea = document.getElementById('repositories');
            if (!textarea?.value) {
                console.log('Manual tab - no textarea value');
                return [];
            }
            const repos = textarea.value.split('\n').filter(r => r.trim()).map(r => r.trim());
            console.log('Manual tab - parsed repos:', repos);
            return repos;
        } else {
            const checkboxes = activeTab.querySelectorAll('input[type="checkbox"]:checked');
            console.log('Browse/Bulk tab - found checkboxes:', checkboxes.length);
            console.log('Checkboxes:', Array.from(checkboxes).map(cb => ({id: cb.id, value: cb.value, checked: cb.checked})));
            
            const repos = Array.from(checkboxes).map(cb => cb.value);
            console.log('Browse/Bulk tab - extracted repo values:', repos);
            return repos;
        }
    }

    removeSelectedRepo(repoName) {
        const checkbox = document.querySelector(`input[value="${repoName}"]`);
        if (checkbox) {
            checkbox.checked = false;
        }
        this.updateSelectedRepos();
    }

    handleFileUpload(event) {
        const file = event.target.files[0];
        const preview = document.getElementById('file-preview');
        
        if (!file || !preview) return;

        const reader = new FileReader();
        reader.onload = (e) => {
            const content = e.target.result;
            const repos = content.split('\n')
                .map(line => line.trim())
                .filter(line => line && !line.startsWith('#'));
            
            preview.innerHTML = `
                <h5>File Preview (${repos.length} repositories)</h5>
                <div class="file-content">
                    ${repos.slice(0, 10).map(repo => `<div>${repo}</div>`).join('')}
                    ${repos.length > 10 ? `<div>... and ${repos.length - 10} more</div>` : ''}
                </div>
                <button type="button" class="btn btn-primary btn-sm" onclick="window.migrationWizard.importFromFile(['${repos.join("','")}'])">
                    Import Repositories
                </button>
            `;
            preview.style.display = 'block';
        };
        
        reader.readAsText(file);
    }

    importFromFile(repos) {
        const textarea = document.getElementById('repositories');
        if (textarea) {
            textarea.value = repos.join('\n');
            this.validateField(textarea);
        }
        
        // Switch to manual tab
        const manualTabBtn = document.querySelector('[data-tab="manual"]');
        if (manualTabBtn) {
            this.switchTab(manualTabBtn);
        }
    }

    updateReviewSection() {
        // Update review section with current form data
        this.saveStepData();
        
        // Source configuration
        document.getElementById('review-ghes-url').textContent = 
            document.getElementById('ghes_base_url')?.value || '-';
        document.getElementById('review-source-org').textContent = 
            document.getElementById('source_org')?.value || '-';
        
        // Target configuration
        document.getElementById('review-target-org').textContent = 
            document.getElementById('target_org')?.value || '-';
        
        // Repositories
        const selectedRepos = this.getSelectedRepositories();
        document.getElementById('review-repo-count').textContent = selectedRepos.length;
        
        const reviewRepos = document.getElementById('review-repos');
        if (reviewRepos) {
            reviewRepos.innerHTML = selectedRepos.map(repo => 
                `<div class="review-repo-item">${repo}</div>`
            ).join('');
        }
        
        // Options
        document.getElementById('review-ghos').textContent = 
            document.getElementById('use_ghos')?.checked ? 'Enabled' : 'Disabled';
        document.getElementById('review-duration').textContent = 
            document.getElementById('max_duration')?.value || 'No limit';
        document.getElementById('review-delete-existing').textContent = 
            document.getElementById('delete_if_exists')?.checked ? 'Enabled' : 'Disabled';
        
        // Scheduling
        const scheduleType = document.querySelector('input[name="schedule_type"]:checked')?.value;
        let scheduleText = 'Immediate';
        if (scheduleType === 'scheduled') {
            const scheduledTime = document.getElementById('scheduled_time')?.value;
            const timeZone = document.getElementById('scheduled_time_zone')?.value;
            if (scheduledTime) {
                scheduleText = `Scheduled for ${scheduledTime}`;
                if (timeZone) scheduleText += ` (${timeZone})`;
            }
        }
        document.getElementById('review-schedule').textContent = scheduleText;
    }

    handleSubmit(event) {
        event.preventDefault();
        
        if (!this.validateCurrentStep()) {
            this.showError('Please fix all errors before submitting');
            return;
        }

        // Prepare form data
        this.saveStepData();
        
        // Set wizard data
        document.getElementById('wizard-data').value = JSON.stringify(this.wizardData);
        
        // Set repositories from selected
        const selectedRepos = this.getSelectedRepositories();
        document.getElementById('repositories').value = selectedRepos.join('\n');
        
        // Show loading state
        const submitBtn = document.getElementById('submit-btn');
        if (submitBtn) {
            submitBtn.classList.add('loading');
            submitBtn.disabled = true;
        }
        
        // Clear draft data on successful submission
        localStorage.removeItem('migration-wizard-draft');
        
        // Submit form
        event.target.submit();
    }

    canNavigateToStep(stepNumber) {
        // Allow navigation to previous steps or next step if current is valid
        return stepNumber <= this.currentStep + 1 && this.validateCurrentStep();
    }

    showError(message) {
        // Create a more user-friendly error display
        const existingError = document.querySelector('.wizard-error-message');
        if (existingError) {
            existingError.remove();
        }

        const errorEl = document.createElement('div');
        errorEl.className = 'wizard-error-message alert alert-error';
        errorEl.innerHTML = `
            <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10"/>
                <line x1="15" y1="9" x2="9" y2="15"/>
                <line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
            <span>${message}</span>
            <button type="button" class="close-btn" onclick="this.parentElement.remove()">
                <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"/>
                    <line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
            </button>
        `;

        const wizardContainer = document.querySelector('.wizard-container');
        if (wizardContainer) {
            wizardContainer.insertBefore(errorEl, wizardContainer.firstChild);
            
            // Auto-remove after 8 seconds
            setTimeout(() => {
                if (errorEl.parentNode) {
                    errorEl.remove();
                }
            }, 8000);
        } else {
            // Fallback to alert if container not found
            alert(message);
        }
    }

    setupAutoSave() {
        // Set up periodic auto-save every 30 seconds
        setInterval(() => {
            if (document.getElementById('migration-wizard-form').style.display !== 'none') {
                this.saveDraft();
            }
        }, 30000);
    }

    initializeValidation() {
        // Add real-time validation to form fields
        document.addEventListener('blur', (e) => {
            if (e.target.form?.id === 'migration-wizard-form') {
                this.validateField(e.target);
            }
        }, true);

        // Show connection test when fields are filled
        document.addEventListener('input', (e) => {
            if (e.target.form?.id === 'migration-wizard-form') {
                setTimeout(() => this.checkConnectionTestVisibility(), 100);
            }
        });
    }
}

// Initialize wizard when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    // Ensure proper initial state - hide all wizard steps immediately
    document.querySelectorAll('#migration-wizard-form .wizard-step').forEach((step, index) => {
        step.classList.remove('active');
        step.style.display = 'none';
        step.style.visibility = 'hidden';
        step.style.height = '0';
        step.style.overflow = 'hidden';
        console.log(`Initial hide of step ${index + 1}:`, step.id);
    });
    
    window.migrationWizard = new MigrationWizard();
});

// CSS injection for progress line animation
document.addEventListener('DOMContentLoaded', () => {
    const style = document.createElement('style');
    style.textContent = `
        .progress-line::after {
            width: var(--wizard-progress, 0%);
        }
    `;
    document.head.appendChild(style);
});
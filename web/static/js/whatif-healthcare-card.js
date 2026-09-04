// What-If Healthcare card: toggle-add-source panel. Extracted from
// components/whatif/healthcare-card.html (U7).

function toggleAddHealthcare(forceOpen) {
    const form = document.getElementById('add-healthcare-form');
    if (typeof forceOpen === 'boolean') {
        form.classList.toggle('hidden', !forceOpen);
        return;
    }
    form.classList.toggle('hidden');
}

function toggleHealthcareFormMode(form) {
    if (!form) {
        return;
    }
    const select = form.querySelector('select[name="person_id"]');
    const manualFields = form.querySelector('[data-manual-healthcare-fields]');
    const preview = form.querySelector('[data-linked-healthcare-preview]');
    const option = select?.selectedOptions?.[0];
    const linked = !!select?.value;

    if (manualFields) {
        manualFields.classList.toggle('hidden', linked);
    }
    if (preview) {
        preview.classList.toggle('hidden', !linked);
        if (linked && option) {
            preview.textContent = option.textContent;
        }
    }
}

// The two "add a source" toggle buttons used to carry inline onclick=
// (U7).
document.addEventListener('DOMContentLoaded', function () {
    document.querySelectorAll('[data-toggle-add-healthcare]').forEach(function (btn) {
        btn.addEventListener('click', toggleAddHealthcare);
    });
});

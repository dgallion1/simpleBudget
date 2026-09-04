// What-If Roth Conversion card: show/hide the conversion-amount fields
// based on the enabled checkbox. Extracted from
// components/whatif/roth-conversion.html (U7).

function toggleRothConversionFields() {
    const enabled = document.getElementById('roth-conversion-enabled').checked;
    const fields = document.getElementById('roth-conversion-fields');
    if (enabled) {
        fields.classList.remove('hidden');
    } else {
        fields.classList.add('hidden');
    }
}

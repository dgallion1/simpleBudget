// File Manager page: drag/drop restore, encryption card (method tabs, YubiKey setup), backup dir open/copy, plaintext export modal, and the sortable/searchable file table. Extracted from pages/filemanager.html (U7).

// Controls that used to carry inline onclick=/onchange= (U7). Elements
// present in the initial page render are wired directly; copyCmd/refresh
// buttons are injected later via innerHTML (YubiKey setup flow), so those
// two are delegated off document instead.
document.addEventListener('DOMContentLoaded', function () {
    var restoreBtn = document.getElementById('restore-btn');
    var restoreInput = document.getElementById('restore-file-input');
    if (restoreBtn && restoreInput) {
        restoreBtn.addEventListener('click', function () { restoreInput.click(); });
        restoreInput.addEventListener('change', function () { handleRestoreFile(this.files); });
    }

    var openExport = document.getElementById('open-plaintext-export-btn');
    if (openExport) openExport.addEventListener('click', openPlaintextExportModal);
    var closeExport = document.getElementById('close-plaintext-export-btn');
    if (closeExport) closeExport.addEventListener('click', closePlaintextExportModal);

    var copyBackupBtn = document.getElementById('copy-backup-dir-btn');
    if (copyBackupBtn) copyBackupBtn.addEventListener('click', copyBackupDir);
    var openBackupBtn = document.getElementById('auto-backup-open-btn');
    if (openBackupBtn) openBackupBtn.addEventListener('click', openBackupDir);

    document.querySelectorAll('[data-select-method]').forEach(function (btn) {
        btn.addEventListener('click', function () { selectMethod(btn.dataset.selectMethod); });
    });
    document.querySelectorAll('[data-toggle-age-option]').forEach(function (input) {
        input.addEventListener('change', toggleAgeOption);
    });

    var yubikeySetupBtn = document.getElementById('yubikey-setup-btn');
    if (yubikeySetupBtn) yubikeySetupBtn.addEventListener('click', setupNewYubiKey);

    // U14: Data Encryption card is collapsed by default behind a disclosure
    // button (aria-expanded + a hidden panel); this is the only toggle for
    // it, so a plain click listener (not delegated) is fine -- the button
    // is present on first paint and never swapped by htmx.
    var encDisclosure = document.getElementById('encryption-disclosure-btn');
    var encPanel = document.getElementById('encryption-panel');
    var encChevron = document.getElementById('encryption-disclosure-chevron');
    if (encDisclosure && encPanel) {
        encDisclosure.addEventListener('click', function () {
            var expand = encDisclosure.getAttribute('aria-expanded') !== 'true';
            encDisclosure.setAttribute('aria-expanded', String(expand));
            encPanel.hidden = !expand;
            if (encChevron) encChevron.style.transform = expand ? 'rotate(180deg)' : '';
        });
    }
});

document.addEventListener('click', function (e) {
    var copyBtn = e.target.closest('[data-copy-cmd]');
    if (copyBtn) { copyCmd(copyBtn.getAttribute('data-copy-cmd')); return; }
    var refreshBtn = e.target.closest('[data-refresh-yubikey]');
    if (refreshBtn) { refreshYubiKeyDetection(); return; }
});

// File-list column sorting (P13). Client-side, typed comparison, survives
// htmx swaps: active sort state lives on window and re-applies on
// htmx:afterSettle, which fires after every #file-list swap (toggle,
// delete, upload).
window.FileManagerSort = (function() {
    var state = { column: null, direction: 'asc' };

    // Comparators operate on the raw data-* attribute strings already
    // coerced to typed values. Files with no date range sort last in
    // ascending order (and first in descending) rather than landing
    // arbitrarily among dated rows.
    function cmp(a, b, column) {
        var av = a.getAttribute('data-' + column) || '';
        var bv = b.getAttribute('data-' + column) || '';
        var dir = state.direction === 'asc' ? 1 : -1;
        var r;
        if (column === 'name') {
            r = av.localeCompare(bv, undefined, { sensitivity: 'base', numeric: true });
        } else if (column === 'rows' || column === 'size') {
            r = (parseInt(av, 10) || 0) - (parseInt(bv, 10) || 0);
        } else if (column === 'enabled') {
            r = (parseInt(av, 10) || 0) - (parseInt(bv, 10) || 0);
        } else if (column === 'mindate') {
            // Empty mindate = no parsed range. Treat as +Infinity so it
            // sorts last ascending, first descending.
            var an = av === '' ? Infinity : av;
            var bn = bv === '' ? Infinity : bv;
            if (an === bn) r = 0;
            else if (an === Infinity) r = 1;
            else if (bn === Infinity) r = -1;
            else r = an < bn ? -1 : 1;
        } else {
            r = 0;
        }
        return r * dir;
    }

    function applySort() {
        var table = document.getElementById('file-manager-table');
        if (!table) return;
        var tbody = table.querySelector('tbody');
        if (!tbody) return;
        // Skip the empty-state row (colspan) — it has no data-* attrs.
        var rows = Array.prototype.slice.call(
            tbody.querySelectorAll('tr[data-name]'));
        if (rows.length === 0) return;

        // Stable sort: Array.prototype.sort is stable in all evergreen
        // browsers and Node 10+.
        rows.sort(function(a, b) { return cmp(a, b, state.column); });

        var frag = document.createDocumentFragment();
        rows.forEach(function(r) { frag.appendChild(r); });
        tbody.appendChild(frag);
        renderIndicators();
    }

    function renderIndicators() {
        var table = document.getElementById('file-manager-table');
        if (!table) return;
        var ths = table.querySelectorAll('th[data-sort]');
        ths.forEach(function(th) {
            var col = th.getAttribute('data-sort');
            var arrow = th.querySelector('[data-sort-arrow]');
            var btn = th.querySelector('[data-sort-btn]');
            if (col === state.column) {
                th.setAttribute('aria-sort',
                    state.direction === 'asc' ? 'ascending' : 'descending');
                if (arrow) {
                    arrow.classList.remove('hidden');
                    arrow.textContent = state.direction === 'asc' ? '▲' : '▼';
                }
            } else {
                th.removeAttribute('aria-sort');
                if (arrow) {
                    arrow.classList.add('hidden');
                    arrow.textContent = '';
                }
            }
        });
    }

    function toggleSort(column) {
        if (state.column === column) {
            state.direction = state.direction === 'asc' ? 'desc' : 'asc';
        } else {
            state.column = column;
            state.direction = 'asc';
        }
        applySort();
    }

    function wireButtons() {
        var table = document.getElementById('file-manager-table');
        if (!table) return;
        var btns = table.querySelectorAll('[data-sort-btn]');
        btns.forEach(function(btn) {
            if (btn.dataset.sortBound === '1') return;
            btn.dataset.sortBound = '1';
            btn.addEventListener('click', function() {
                toggleSort(btn.getAttribute('data-sort-btn'));
            });
        });
        // Re-apply any persisted sort after a swap rebuilt the table.
        if (state.column) applySort();
    }

    return {
        init: function() { wireButtons(); },
        reapply: function() { wireButtons(); }
    };
})();

document.addEventListener('DOMContentLoaded', function() {
    if (window.FileManagerSort) window.FileManagerSort.init();
});
// htmx:afterSettle fires after every swap into #file-list (toggle, delete,
// upload) rebuilds the table markup. Re-wire the header buttons and
// re-apply the persisted sort so the ordering survives.
document.body.addEventListener('htmx:afterSettle', function() {
    if (window.FileManagerSort) window.FileManagerSort.reapply();
});
// htmx:afterSwap fires immediately after the delete button or the
// enable/disable checkbox replaces #file-list's contents. The element that
// had focus (the deleted row's button, or the toggled row's checkbox) is
// gone from the DOM at that point, so focus would otherwise silently revert
// to <body>. #file-list itself is the hx-target, not part of the swapped
// innerHTML, so it survives every swap and is a stable landing spot
// regardless of whether the action left any rows in the table (tabindex="-1"
// on the div makes it programmatically focusable without adding a tab stop).
document.body.addEventListener('htmx:afterSwap', function(evt) {
    if (evt.detail && evt.detail.target && evt.detail.target.id === 'file-list') {
        evt.detail.target.focus();
    }
});

// Auto-backup status card
(function() {
    function formatBytes(n) {
        if (n >= 1048576) return (n / 1048576).toFixed(1) + ' MB';
        return (n / 1024).toFixed(1) + ' KB';
    }

    function refreshAutoBackupStatus() {
        fetch('/backup/status')
            .then(function(r) { return r.json(); })
            .then(function(s) {
                var toggle = document.getElementById('auto-backup-toggle');
                var label  = document.getElementById('auto-backup-toggle-label');
                var last   = document.getElementById('auto-backup-last');
                var count  = document.getElementById('auto-backup-count');
                var bytes  = document.getElementById('auto-backup-bytes');
                var dir    = document.getElementById('auto-backup-dir');
                var err    = document.getElementById('auto-backup-error');

                if (!toggle) return; // card not on this page

                toggle.checked = !!s.enabled;
                label.textContent = s.enabled ? 'Enabled' : 'Disabled';
                last.textContent  = s.ts || 'never';
                count.textContent = s.snapshot_count || 0;
                bytes.textContent = formatBytes(s.total_bytes || 0);
                dir.textContent   = s.dir || '(not configured)';

                if (s.last_error) {
                    err.textContent = 'Last attempt failed: ' + s.last_error;
                    err.classList.remove('hidden');
                } else {
                    err.classList.add('hidden');
                }
            })
            .catch(function() {
                var label = document.getElementById('auto-backup-toggle-label');
                if (label) label.textContent = 'Unavailable';
            });
    }

    document.addEventListener('DOMContentLoaded', function() {
        var toggle = document.getElementById('auto-backup-toggle');
        if (!toggle) return;

        refreshAutoBackupStatus();
        setInterval(refreshAutoBackupStatus, 30000);

        toggle.addEventListener('change', function(e) {
            var body = new URLSearchParams({ enabled: e.target.checked ? 'true' : 'false' });
            fetch('/backup/auto-enabled', {
                method: 'POST',
                headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
                body: body.toString()
            }).then(refreshAutoBackupStatus);
        });
    });

    window.copyBackupDir = function() {
        var dir = document.getElementById('auto-backup-dir');
        if (!dir) return;
        var text = dir.textContent || '';
        if (!text || text === '…' || text === '(not configured)') return;
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(text).then(function() {
                showToast('Path copied', 'success');
            }).catch(function() {
                showToast('Failed to copy', 'error');
            });
        } else {
            // Fallback: select the text so the user can copy manually
            var range = document.createRange();
            range.selectNodeContents(dir);
            var sel = window.getSelection();
            sel.removeAllRanges();
            sel.addRange(range);
            showToast('Press Ctrl+C to copy', 'info');
        }
    };

    window.openBackupDir = function() {
        fetch('/backup/open-dir', { method: 'POST' })
            .then(function(r) {
                if (!r.ok) {
                    return r.text().then(function(t) { throw new Error(t || ('HTTP ' + r.status)); });
                }
                showToast('Opened in file manager', 'success');
            })
            .catch(function(err) {
                showToast('Could not open: ' + err.message, 'error');
            });
    };
})();

// Drag and drop support
document.addEventListener('DOMContentLoaded', function() {
    const restoreBtn = document.getElementById('restore-btn');

    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        document.addEventListener(eventName, e => e.preventDefault(), false);
    });

    document.addEventListener('dragenter', function(e) {
        if (e.dataTransfer.types.includes('Files')) {
            restoreBtn.classList.add('ring-2', 'ring-accent', 'bg-accent-soft', 'bg-accent-soft');
        }
    });

    document.addEventListener('dragleave', function(e) {
        if (!e.relatedTarget || e.relatedTarget === document.documentElement) {
            restoreBtn.classList.remove('ring-2', 'ring-accent', 'bg-accent-soft', 'bg-accent-soft');
        }
    });

    document.addEventListener('drop', function(e) {
        restoreBtn.classList.remove('ring-2', 'ring-accent', 'bg-accent-soft', 'bg-accent-soft');
        if (e.dataTransfer.files.length > 0) {
            handleRestoreFile(e.dataTransfer.files);
        }
    });
});

function handleRestoreFile(files) {
    if (files.length === 0) return;

    const file = files[0];
    if (!file.name.toLowerCase().endsWith('.zip')) {
        showToast('Only ZIP backup files are accepted', 'error');
        return;
    }

    restoreBackup(file);
}

function showToast(message, type) {
    const toast = document.getElementById('restore-toast');
    toast.textContent = message;
    toast.className = 'fixed bottom-4 right-4 px-4 py-2 rounded-lg shadow-lg text-sm font-medium';

    if (type === 'error') {
        toast.classList.add('bg-negative-strong', 'text-white');
    } else if (type === 'success') {
        toast.classList.add('bg-positive-strong', 'text-white');
    } else {
        toast.classList.add('bg-gray-800', 'text-white');
    }

    setTimeout(() => toast.classList.add('hidden'), 3000);
}

function setRestoreButtonState(state) {
    const btn = document.getElementById('restore-btn');
    const text = document.getElementById('restore-btn-text');

    if (state === 'loading') {
        btn.disabled = true;
        btn.classList.add('opacity-50', 'cursor-not-allowed');
        text.textContent = 'Restoring...';
    } else {
        btn.disabled = false;
        btn.classList.remove('opacity-50', 'cursor-not-allowed');
        text.textContent = 'Restore';
    }
}

function restoreBackup(file) {
    if (!confirm("Restore replaces ALL current data with this backup's contents — any file not in the backup will be deleted. A safety snapshot is taken first. Continue?")) {
        document.getElementById('restore-file-input').value = '';
        return;
    }

    setRestoreButtonState('loading');

    const formData = new FormData();
    formData.append('file', file);

    fetch('/restore', {
        method: 'POST',
        body: formData
    })
        .then(response => {
            if (!response.ok) {
                return response.text().then(text => { throw new Error(text || 'Restore failed'); });
            }
            return response.text();
        })
        .then(message => {
            // Reset the button before the reload: if the reload is blocked
            // (beforeunload, extension), the page must not be left with a
            // permanently disabled "Restoring..." button.
            setRestoreButtonState('default');
            showToast(message || 'Restored successfully!', 'success');
            document.getElementById('restore-file-input').value = '';
            // Give the user time to read the server's summary (skipped /
            // failed counts) before the reload wipes the toast.
            setTimeout(() => window.location.reload(), 2500);
        })
        .catch(err => {
            setRestoreButtonState('default');
            showToast(err.message, 'error');
        });
}

// Plaintext export ("break-glass") modal
function openPlaintextExportModal() {
    var m = document.getElementById('plaintext-modal');
    if (!m) return;
    m.classList.remove('hidden');
    var first = m.querySelector('input');
    if (first) { first.value = ''; first.focus(); }
    var err = document.getElementById('plaintext-error');
    if (err) { err.textContent = ''; err.classList.add('hidden'); }
}
function closePlaintextExportModal() {
    var m = document.getElementById('plaintext-modal');
    if (!m) return;
    m.classList.add('hidden');
    m.querySelectorAll('input').forEach(function(i) { i.value = ''; });
    var err = document.getElementById('plaintext-error');
    if (err) { err.textContent = ''; err.classList.add('hidden'); }
}
document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') {
        var m = document.getElementById('plaintext-modal');
        if (m && !m.classList.contains('hidden')) closePlaintextExportModal();
    }
});
document.addEventListener('DOMContentLoaded', function() {
    var form = document.getElementById('plaintext-form');
    if (!form) return;
    form.addEventListener('submit', function(e) {
        e.preventDefault();
        var btn = document.getElementById('plaintext-submit');
        var err = document.getElementById('plaintext-error');
        err.classList.add('hidden');
        var origText = btn.textContent;
        btn.disabled = true;
        btn.textContent = 'Decrypting…';

        var fd = new FormData(form);
        var params = new URLSearchParams();
        fd.forEach(function(value, key) { params.append(key, value); });

        fetch('/backup/plaintext', {
            method: 'POST',
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
            body: params
        }).then(function(r) {
            if (!r.ok) {
                return r.text().then(function(t) { throw new Error(t || ('HTTP ' + r.status)); });
            }
            var disp = r.headers.get('Content-Disposition') || '';
            var m = disp.match(/filename=([^;]+)/);
            var filename = m ? m[1].trim() : 'budget_plaintext.zip';
            return r.blob().then(function(blob) {
                var url = URL.createObjectURL(blob);
                var a = document.createElement('a');
                a.href = url;
                a.download = filename;
                document.body.appendChild(a);
                a.click();
                document.body.removeChild(a);
                URL.revokeObjectURL(url);
                showToast('Plaintext export downloaded', 'success');
                closePlaintextExportModal();
            });
        }).catch(function(ex) {
            err.textContent = ex.message;
            err.classList.remove('hidden');
        }).finally(function() {
            btn.disabled = false;
            btn.textContent = origText;
        });
    });
});

// Method selection
let currentEncryptionMethod = 'password';
let detectedKeys = null;

function selectMethod(method) {
    currentEncryptionMethod = method;

    // Update tab styles
    document.querySelectorAll('.method-tab').forEach(tab => {
        tab.classList.remove('border-accent', 'text-accent', 'text-accent');
        tab.classList.add('border-transparent', 'text-gray-600', 'dark:text-gray-400');
    });

    const activeTab = document.getElementById('tab-' + method);
    if (activeTab) {
        activeTab.classList.remove('border-transparent', 'text-gray-600', 'dark:text-gray-400');
        activeTab.classList.add('border-accent', 'text-accent', 'text-accent');
    }

    // Show/hide forms
    document.querySelectorAll('.method-form').forEach(form => {
        if (form.dataset.method === method) {
            form.classList.remove('hidden');
        } else {
            form.classList.add('hidden');
        }
    });
}

function toggleAgeOption() {
    const generateSelected = document.querySelector('input[name="age_option"][value="generate"]').checked;
    const pathInput = document.getElementById('age-path-input');
    if (generateSelected) {
        pathInput.classList.add('hidden');
    } else {
        pathInput.classList.remove('hidden');
    }
}

// Load detected keys on page load
document.addEventListener('DOMContentLoaded', function() {
    fetch('/encryption/detect-keys')
        .then(response => response.json())
        .then(data => {
            detectedKeys = data;

            // Populate SSH key select
            const sshSelect = document.getElementById('ssh-key-select');
            if (sshSelect && data.ssh_keys && data.ssh_keys.length > 0) {
                sshSelect.innerHTML = '';
                data.ssh_keys.forEach(key => {
                    const option = document.createElement('option');
                    option.value = key.path;
                    option.textContent = `${key.path} (${key.type}${key.encrypted ? ', encrypted' : ''})`;
                    option.dataset.encrypted = key.encrypted;
                    sshSelect.appendChild(option);
                });

                // Show passphrase field if first key is encrypted
                if (data.ssh_keys[0].encrypted) {
                    document.getElementById('ssh-passphrase-field').classList.remove('hidden');
                }

                sshSelect.addEventListener('change', function() {
                    const selectedOption = sshSelect.options[sshSelect.selectedIndex];
                    const passphraseField = document.getElementById('ssh-passphrase-field');
                    if (selectedOption.dataset.encrypted === 'true') {
                        passphraseField.classList.remove('hidden');
                    } else {
                        passphraseField.classList.add('hidden');
                    }
                });
            } else if (sshSelect) {
                sshSelect.innerHTML = '<option value="">No SSH keys found</option>';
            }

            // Update YubiKey UI based on plugin status
            if (data.yubikey_installed) {
                updateYubiKeyUI(data);
            } else {
                document.getElementById('yubikey-status').innerHTML = `
                    <div class="bg-warning-soft border border-warning rounded-lg p-3">
                        <p class="text-sm text-warning mb-2">
                            <strong>age-plugin-yubikey</strong> is required but not installed.
                        </p>
                        <div class="text-body-sm text-warning space-y-1">
                            <div class="flex items-center gap-1">
                                <strong>macOS:</strong>
                                <code class="bg-warning-soft px-1 rounded">brew install age-plugin-yubikey</code>
                                <button data-copy-cmd="brew install age-plugin-yubikey" class="p-0.5 hover:bg-warning-strong rounded" title="Copy" aria-label="Copy">
                                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                                </button>
                            </div>
                            <div class="flex items-center gap-1">
                                <strong>Ubuntu/Debian:</strong>
                                <code class="bg-warning-soft px-1 rounded">sudo apt install pcscd</code>
                                <button data-copy-cmd="sudo apt install pcscd" class="p-0.5 hover:bg-warning-strong rounded" title="Copy" aria-label="Copy">
                                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                                </button>
                                <span class="ml-1">then download from <a href="https://github.com/str4d/age-plugin-yubikey/releases" target="_blank" class="underline hover:no-underline">Releases</a></span>
                            </div>
                            <div class="flex items-center gap-1">
                                <strong>Arch Linux:</strong>
                                <code class="bg-warning-soft px-1 rounded">pacman -S age-plugin-yubikey</code>
                                <button data-copy-cmd="pacman -S age-plugin-yubikey" class="p-0.5 hover:bg-warning-strong rounded" title="Copy" aria-label="Copy">
                                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"></path></svg>
                                </button>
                            </div>
                            <div><strong>Windows:</strong> Download from <a href="https://github.com/str4d/age-plugin-yubikey/releases" target="_blank" class="underline hover:no-underline">GitHub Releases</a></div>
                        </div>
                    </div>`;
                document.getElementById('yubikey-submit-btn').disabled = true;
            }
        })
        .catch(() => {
            const sshSelect = document.getElementById('ssh-key-select');
            if (sshSelect) {
                sshSelect.innerHTML = '<option value="">Failed to detect keys</option>';
            }
        });
});

function updateYubiKeyUI(data) {
    const statusDiv = document.getElementById('yubikey-status');
    const identitiesDiv = document.getElementById('yubikey-identities');
    const setupDiv = document.getElementById('yubikey-setup');
    const readyDiv = document.getElementById('yubikey-ready');
    const select = document.getElementById('yubikey-select');
    const submitBtn = document.getElementById('yubikey-submit-btn');

    if (data.yubikey_identities && data.yubikey_identities.length > 0) {
        // Show the select dropdown with available identities
        statusDiv.classList.add('hidden');
        identitiesDiv.classList.remove('hidden');
        setupDiv.classList.add('hidden');

        select.innerHTML = '<option value="">Select an identity...</option>';
        data.yubikey_identities.forEach((recipient, index) => {
            const option = document.createElement('option');
            option.value = recipient;
            option.textContent = recipient.substring(0, 40) + '...';
            option.title = recipient;
            select.appendChild(option);
        });

        select.addEventListener('change', function() {
            if (select.value) {
                // Fetch the identity for this recipient
                fetchYubiKeyIdentity(select.value);
            } else {
                readyDiv.classList.add('hidden');
                submitBtn.disabled = true;
            }
        });
    } else {
        // No existing identities, show setup option
        statusDiv.classList.add('hidden');
        identitiesDiv.classList.add('hidden');
        setupDiv.classList.remove('hidden');
    }
}

function fetchYubiKeyIdentity(recipient) {
    const readyDiv = document.getElementById('yubikey-ready');
    const readyText = document.getElementById('yubikey-ready-text');
    const submitBtn = document.getElementById('yubikey-submit-btn');
    const identityInput = document.getElementById('yubikey-identity-input');
    const recipientInput = document.getElementById('yubikey-recipient-input');

    // For now, we'll need to get the identity via the API
    // This requires the user to touch the YubiKey
    fetch('/encryption/yubikey-identity?recipient=' + encodeURIComponent(recipient))
        .then(response => response.json())
        .then(data => {
            if (data.identity) {
                identityInput.value = data.identity;
                recipientInput.value = recipient;
                readyText.textContent = 'YubiKey ready: ' + recipient.substring(0, 30) + '...';
                readyDiv.classList.remove('hidden');
                submitBtn.disabled = false;
            } else {
                throw new Error('Failed to get identity');
            }
        })
        .catch(err => {
            document.getElementById('yubikey-encryption-error').textContent = 'Failed to get YubiKey identity. Make sure your YubiKey is connected.';
            document.getElementById('yubikey-encryption-error').classList.remove('hidden');
        });
}

function copyCmd(text) {
    navigator.clipboard.writeText(text).then(() => {
        showToast('Copied to clipboard', 'success');
    }).catch(() => {
        showToast('Failed to copy', 'error');
    });
}

function setupNewYubiKey() {
    const setupDiv = document.getElementById('yubikey-setup');

    fetch('/encryption/yubikey-setup', { method: 'POST' })
        .then(response => response.json())
        .then(data => {
            if (data.setup_command) {
                // Show terminal setup instructions
                setupDiv.innerHTML = `
                    <div class="bg-accent-soft border border-accent rounded-lg p-4">
                        <p class="text-sm text-accent mb-3">
                            <strong>Terminal Setup Required</strong><br>
                            YubiKey setup requires terminal interaction. Run this command:
                        </p>
                        <div class="flex items-center gap-2 mb-3">
                            <code class="flex-1 bg-gray-800 text-positive px-3 py-2 rounded font-mono text-sm">${data.setup_command}</code>
                            <button data-copy-cmd="${data.setup_command.replace(/"/g, '&quot;')}" class="p-2 bg-gray-700 hover:bg-gray-600 rounded" title="Copy command" aria-label="Copy command">
                                <svg class="w-4 h-4 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"/>
                                </svg>
                            </button>
                        </div>
                        <p class="text-body-sm text-accent mb-3">
                            Follow the prompts in your terminal, then click Refresh below.
                        </p>
                        <button data-refresh-yubikey class="w-full px-4 py-2 bg-accent-strong hover:bg-accent-strong text-white rounded-lg font-medium transition-colors">
                            Refresh YubiKey Detection
                        </button>
                    </div>
                `;
            }
        })
        .catch(err => {
            const errorDiv = document.getElementById('yubikey-encryption-error');
            errorDiv.textContent = err.message;
            errorDiv.classList.remove('hidden');
        });
}

function refreshYubiKeyDetection() {
    // Re-fetch encryption methods to detect new YubiKey identity
    fetch('/encryption/methods')
        .then(response => response.json())
        .then(data => {
            if (data.yubikey_installed) {
                updateYubiKeyUI(data);
                if (data.yubikey_identities && data.yubikey_identities.length > 0) {
                    showToast('YubiKey identity detected!', 'success');
                } else {
                    showToast('No YubiKey identity found. Please run the setup command first.', 'error');
                }
            }
        })
        .catch(err => {
            showToast('Failed to detect YubiKey: ' + err.message, 'error');
        });
}

// Form submission handlers
function submitEncryptionForm(form, errorDivId) {
    const formData = new FormData(form);
    const params = new URLSearchParams();

    for (const [key, value] of formData.entries()) {
        params.append(key, value);
    }

    // Add generate_new flag for age method
    if (currentEncryptionMethod === 'age') {
        const generateSelected = document.querySelector('input[name="age_option"][value="generate"]').checked;
        params.append('generate_new', generateSelected ? 'true' : 'false');
    }

    const btn = form.querySelector('button[type="submit"]');
    const errorDiv = document.getElementById(errorDivId);

    errorDiv.classList.add('hidden');
    btn.disabled = true;
    const originalText = btn.textContent;
    btn.textContent = 'Encrypting...';

    fetch('/encryption/enable', {
        method: 'POST',
        headers: {'Content-Type': 'application/x-www-form-urlencoded'},
        body: params
    })
    .then(response => {
        if (!response.ok) {
            return response.text().then(text => { throw new Error(text); });
        }
        return response.text();
    })
    .then(() => {
        showToast('Encryption enabled successfully!', 'success');
        setTimeout(() => window.location.reload(), 1000);
    })
    .catch(err => {
        btn.disabled = false;
        btn.textContent = originalText;
        errorDiv.textContent = err.message;
        errorDiv.classList.remove('hidden');
        // Move focus to the first non-hidden field in the form so the
        // aria-describedby'd error is reachable immediately after failure.
        const firstField = form.querySelector('input:not([type=hidden]), select');
        if (firstField) firstField.focus();
    });
}

// Encryption form handlers
document.addEventListener('DOMContentLoaded', function() {
    const unlockForm = document.getElementById('unlock-form');
    const enableForm = document.getElementById('enable-encryption-form');
    const disableForm = document.getElementById('disable-encryption-form');
    const ageForm = document.getElementById('age-encryption-form');
    const sshForm = document.getElementById('ssh-encryption-form');
    const yubikeyForm = document.getElementById('yubikey-encryption-form');

    // Unlock form handler
    if (unlockForm) {
        unlockForm.addEventListener('submit', function(e) {
            e.preventDefault();
            const btn = document.getElementById('unlock-btn');
            const errorDiv = document.getElementById('unlock-error');
            const passwordInput = document.getElementById('unlock-password');
            const password = passwordInput ? passwordInput.value : '';

            errorDiv.classList.add('hidden');
            btn.disabled = true;
            const originalText = btn.textContent;
            btn.textContent = 'Unlocking...';

            const params = new URLSearchParams();
            params.append('password', password);

            fetch('/unlock', {
                method: 'POST',
                headers: {'Content-Type': 'application/x-www-form-urlencoded'},
                body: params
            })
            .then(response => {
                if (!response.ok) {
                    return response.text().then(text => { throw new Error(text || 'Unlock failed'); });
                }
                // Reload page to show unlocked state
                window.location.reload();
            })
            .catch(err => {
                btn.disabled = false;
                btn.textContent = originalText;
                errorDiv.textContent = err.message;
                errorDiv.classList.remove('hidden');
                if (passwordInput) {
                    passwordInput.focus();
                    passwordInput.select();
                }
            });
        });
    }

    if (enableForm) {
        enableForm.addEventListener('submit', function(e) {
            e.preventDefault();
            const btn = document.getElementById('enable-encryption-btn');
            const errorDiv = document.getElementById('enable-encryption-error');

            // Get values directly from inputs
            const passwordInput = enableForm.querySelector('input[name="password"]');
            const confirmInput = enableForm.querySelector('input[name="confirmPassword"]');
            const password = passwordInput.value;
            const confirmPassword = confirmInput.value;

            if (password.length < 8) {
                errorDiv.textContent = 'Password must be at least 8 characters';
                errorDiv.classList.remove('hidden');
                passwordInput.focus();
                return;
            }

            if (password !== confirmPassword) {
                errorDiv.textContent = 'Passwords do not match';
                errorDiv.classList.remove('hidden');
                confirmInput.focus();
                return;
            }

            submitEncryptionForm(enableForm, 'enable-encryption-error');
        });
    }

    if (ageForm) {
        ageForm.addEventListener('submit', function(e) {
            e.preventDefault();
            submitEncryptionForm(ageForm, 'age-encryption-error');
        });
    }

    if (sshForm) {
        sshForm.addEventListener('submit', function(e) {
            e.preventDefault();
            const sshSelect = document.getElementById('ssh-key-select');
            if (!sshSelect.value) {
                document.getElementById('ssh-encryption-error').textContent = 'Please select an SSH key';
                document.getElementById('ssh-encryption-error').classList.remove('hidden');
                sshSelect.focus();
                return;
            }
            submitEncryptionForm(sshForm, 'ssh-encryption-error');
        });
    }

    if (yubikeyForm) {
        yubikeyForm.addEventListener('submit', function(e) {
            e.preventDefault();
            submitEncryptionForm(yubikeyForm, 'yubikey-encryption-error');
        });
    }

    if (disableForm) {
        disableForm.addEventListener('submit', function(e) {
            e.preventDefault();
            const btn = document.getElementById('disable-encryption-btn');
            const errorDiv = document.getElementById('disable-encryption-error');
            const passwordInput = disableForm.querySelector('input[name="password"]');

            // Password is optional for non-password methods
            const password = passwordInput ? passwordInput.value : '';

            if (!confirm('Are you sure you want to disable encryption? Your data will be stored unencrypted.')) {
                return;
            }

            errorDiv.classList.add('hidden');
            btn.disabled = true;
            btn.textContent = 'Decrypting...';

            // Use URLSearchParams for form data
            const params = new URLSearchParams();
            params.append('password', password);

            fetch('/encryption/disable', {
                method: 'POST',
                headers: {'Content-Type': 'application/x-www-form-urlencoded'},
                body: params
            })
            .then(response => {
                if (!response.ok) {
                    return response.text().then(text => { throw new Error(text); });
                }
                return response.text();
            })
            .then(() => {
                showToast('Encryption disabled successfully!', 'success');
                setTimeout(() => window.location.reload(), 1000);
            })
            .catch(err => {
                btn.disabled = false;
                btn.textContent = 'Disable Encryption';
                errorDiv.textContent = err.message;
                errorDiv.classList.remove('hidden');
                if (passwordInput) passwordInput.focus();
            });
        });
    }
});

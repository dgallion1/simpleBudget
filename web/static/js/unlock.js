// Unlock page: detects the configured auth method and adapts the form
// (password/age/ssh/yubikey), submits the unlock request. Extracted
// from pages/unlock.html (U7).

        let currentMethod = 'password';

        // Detect the current auth method on page load
        document.addEventListener('DOMContentLoaded', function() {
            fetch('/encryption/config')
                .then(response => response.json())
                .then(data => {
                    if (data.method) {
                        currentMethod = data.method;
                        updateUIForMethod(data.method);
                    }
                })
                .catch(() => {
                    // Default to password if we can't detect
                });
        });

        function updateUIForMethod(method) {
            const description = document.getElementById('unlock-description');
            const passwordField = document.getElementById('password-field');
            const passwordInput = document.getElementById('password');
            const unlockBtn = document.getElementById('unlock-btn');
            const methodIndicator = document.getElementById('method-indicator');
            const methodName = document.getElementById('method-name');

            switch (method) {
                case 'age':
                    description.textContent = 'Your age identity file will be used to unlock';
                    passwordField.classList.add('hidden');
                    passwordInput.required = false;
                    unlockBtn.textContent = 'Unlock with Age Identity';
                    methodIndicator.classList.remove('hidden');
                    methodName.textContent = 'Age Identity File';
                    break;

                case 'ssh':
                    description.textContent = 'Enter SSH key passphrase (if encrypted) to unlock';
                    passwordInput.placeholder = 'SSH Key Passphrase (leave empty if unencrypted)';
                    passwordInput.required = false;
                    unlockBtn.textContent = 'Unlock with SSH Key';
                    methodIndicator.classList.remove('hidden');
                    methodName.textContent = 'SSH Key';
                    break;

                case 'yubikey':
                    description.textContent = 'Click the button below, then touch your YubiKey to unlock';
                    passwordField.classList.add('hidden');
                    passwordInput.required = false;
                    unlockBtn.textContent = 'Unlock with YubiKey';
                    methodIndicator.classList.remove('hidden');
                    methodName.textContent = 'YubiKey Hardware Key';
                    break;

                default:
                    // Password method - use defaults
                    break;
            }
        }

        document.getElementById('unlock-form').addEventListener('submit', function(e) {
            e.preventDefault();

            const btn = document.getElementById('unlock-btn');
            const errorDiv = document.getElementById('error-message');
            const password = document.getElementById('password').value;

            // Only require password for password method
            if (currentMethod === 'password' && !password) {
                errorDiv.textContent = 'Password is required';
                errorDiv.classList.remove('hidden');
                return;
            }

            errorDiv.classList.add('hidden');
            btn.disabled = true;
            const originalText = btn.textContent;
            btn.textContent = 'Unlocking...';

            const params = new URLSearchParams();
            params.append('password', password || '');

            fetch('/unlock', {
                method: 'POST',
                headers: {'Content-Type': 'application/x-www-form-urlencoded'},
                body: params
            })
            .then(response => {
                if (!response.ok) {
                    return response.text().then(text => { throw new Error(text || 'Unlock failed'); });
                }
                // Redirect to dashboard on success
                window.location.href = '/dashboard';
            })
            .catch(err => {
                btn.disabled = false;
                btn.textContent = originalText;
                errorDiv.textContent = err.message;
                errorDiv.classList.remove('hidden');
                if (currentMethod === 'password' || currentMethod === 'ssh') {
                    document.getElementById('password').select();
                }
            });
        });

export function renderedClipboard(element: HTMLElement) {
    return {
        plain: (element.innerText || element.textContent || '').replace(/\n{3,}/g, '\n\n').trim(),
        html: element.innerHTML,
    };
}

export async function writeRichClipboard(plain: string, html = ''): Promise<void> {
    if (navigator.clipboard && window.ClipboardItem && window.isSecureContext && html) {
        try {
            await navigator.clipboard.write([new window.ClipboardItem({
                'text/plain': new Blob([plain], { type: 'text/plain' }),
                'text/html': new Blob([html], { type: 'text/html' }),
            })]);
            return;
        } catch (error) {
            if (!navigator.clipboard.writeText) throw error;
        }
    }
    if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(plain);
        return;
    }
    const focused = document.activeElement;
    const textarea = document.createElement('textarea');
    textarea.value = plain;
    textarea.setAttribute('readonly', '');
    textarea.style.position = 'fixed';
    textarea.style.opacity = '0';
    document.body.appendChild(textarea);
    try {
        textarea.select();
        if (!document.execCommand('copy')) throw new Error('Копирование не поддерживается браузером.');
    } finally {
        textarea.remove();
        if (focused instanceof HTMLElement) focused.focus();
    }
}

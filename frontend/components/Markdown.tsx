import { useRef, useState, type ReactNode } from 'react';
import { Icon, safeUrl } from './Common';
function inline(value: string): ReactNode[] { return value.split(/(`[^`\n]+`|\*\*[^*\n]+\*\*|\*[^*\n]+\*|\[[^\]]+\]\([^\s)]+\))/g).map((p, i) => { if (p.startsWith('`'))
    return <code key={i}>{p.slice(1, -1)}</code>; if (p.startsWith('**'))
    return <strong key={i}>{p.slice(2, -2)}</strong>; if (p.startsWith('*'))
    return <em key={i}>{p.slice(1, -1)}</em>; const m = p.match(/^\[([^\]]+)\]\(([^)]+)\)$/); if (m && (safeUrl(m[2]) || /^[A-Z_]+\.md(?:#[\w-]+)?$/.test(m[2])))
    return <a key={i} href={safeUrl(m[2]) || m[2]} target="_blank" rel="noopener noreferrer">{m[1]}</a>; return p; }); }
export function Markdown({ value, empty = 'Комментарий пока пустой.', className = 'rendered-md' }: {
    value: string;
    empty?: string;
    className?: string;
}) { const lines = String(value || '').replace(/\r/g, '').split('\n'), nodes: ReactNode[] = []; const cells = (l: string) => l.trim().replace(/^\||\|$/g, '').split(/(?<!\\)\|/).map(c => c.trim().replace(/\\\|/g, '|')); for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^<!--/.test(line))
        continue;
    if (line.startsWith('```')) {
        const code = [];
        while (++i < lines.length && !lines[i].startsWith('```'))
            code.push(lines[i]);
        nodes.push(<pre key={i}><code>{code.join('\n')}</code></pre>);
        continue;
    }
    if (line.includes('|') && /^\s*\|?\s*:?-{3,}/.test(lines[i + 1] || '')) {
        const header = cells(line), rows: string[][] = [];
        i++;
        while (i + 1 < lines.length && lines[i + 1].includes('|') && lines[i + 1].trim())
            rows.push(cells(lines[++i]));
        nodes.push(<div className="docs-table-wrap" key={i}><table><thead><tr>{header.map((c, j) => <th key={j}>{inline(c)}</th>)}</tr></thead><tbody>{rows.map((r, j) => <tr key={j}>{r.map((c, k) => <td key={k}>{inline(c)}</td>)}</tr>)}</tbody></table></div>);
        continue;
    }
    const h = line.match(/^(#{1,4})\s+(.+)/);
    if (h) {
        const Tag = ('h' + h[1].length) as 'h1' | 'h2' | 'h3' | 'h4';
        nodes.push(<Tag key={i}>{inline(h[2])}</Tag>);
        continue;
    }
    const list = line.match(/^\s*([-*]|\d+\.)\s+(.+)/);
    if (list) {
        const ordered = /\d/.test(list[1]), items = [list[2]];
        while (i + 1 < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i + 1]))
            items.push(lines[++i].replace(/^\s*([-*]|\d+\.)\s+/, ''));
        const Tag = ordered ? 'ol' : 'ul';
        nodes.push(<Tag key={i}>{items.map((v, j) => <li key={j}>{inline(v)}</li>)}</Tag>);
        continue;
    }
    if (/^>\s?/.test(line))
        nodes.push(<blockquote key={i}>{inline(line.replace(/^>\s?/, ''))}</blockquote>);
    else if (line.trim())
        nodes.push(<p key={i}>{inline(line)}</p>);
} return <div className={className}>{nodes.length ? nodes : <div className="history-empty">{empty}</div>}</div>; }
export function MarkdownEditor({ value, onChange, limit = 1000, label = 'Комментарий', className = '', footer, placeholder }: {
    value: string;
    onChange: (v: string) => void;
    limit?: number;
    label?: string;
    className?: string;
    footer?: ReactNode;
    placeholder?: string;
}) {
    const [preview, setPreview] = useState(false);
    const ref = useRef<HTMLTextAreaElement>(null);
    const isResume = className.includes('resume');
    const count = Array.from(value).length;
    const tools = [
        { name: 'bold', label: 'Жирный', before: '**', after: '**' },
        { name: 'italic', label: 'Курсив', before: '*', after: '*' },
        { name: 'heading', label: 'Заголовок', before: '## ', after: '' },
        { name: 'list', label: 'Список', before: '- ', after: '' },
        { name: 'link', label: 'Ссылка', before: '[', after: '](https://)' },
    ] as const;
    const orderedTools = isResume ? [tools[2], tools[0], tools[1], ...tools.slice(3)] : tools;
    function insert(before: string, after = '') {
        const el = ref.current;
        if (!el) return;
        const a = el.selectionStart, b = el.selectionEnd;
        onChange(value.slice(0, a) + before + value.slice(a, b) + after + value.slice(b));
        requestAnimationFrame(() => {
            el.focus();
            el.setSelectionRange(a + before.length, b + before.length);
        });
    }
    return <div className={'markdown-editor' + (isResume ? '' : ' crm-editor')}>
        <div className={isResume ? 'resume-toolbar' : 'md-toolbar'} aria-label={'Форматирование: ' + label}>
            {orderedTools.map(tool => <button className={isResume ? undefined : 'md-tool'} type="button"
                key={tool.name} title={tool.label} aria-label={tool.label} disabled={preview}
                onClick={() => insert(tool.before, tool.after)}>
                {isResume ? tool.name === 'bold' ? <b>{tool.label}</b> : tool.name === 'italic' ? <i>{tool.label}</i> : tool.label : <Icon name={tool.name}/>}
            </button>)}
            <button className={isResume ? undefined : 'md-tool preview-tool'} type="button"
                aria-pressed={preview} onClick={() => setPreview(!preview)}>
                {preview ? 'Редактировать' : 'Предпросмотр'}
            </button>
        </div>
        {preview ? <div className={isResume ? 'resume-preview' : 'crm-preview show'}
            aria-label={'Предпросмотр: ' + label}><Markdown className="markdown-content" value={value}/></div>
            : <textarea className={className || 'crm-comment'} ref={ref} aria-label={label} placeholder={placeholder}
                aria-invalid={count > limit} value={value} onChange={e => onChange(e.target.value)}/>}
        <div className={isResume ? 'resume-footer' : 'crm-editor-foot'}>
            {!isResume && <span>Markdown</span>}
            <span className={'count' + (count > limit ? ' error' : '')}>
                {count.toLocaleString('ru-RU')} / {limit.toLocaleString('ru-RU')}{isResume && ' символов'}
            </span>
            {footer}
        </div>
    </div>;
}

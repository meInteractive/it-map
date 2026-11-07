import {useEffect,useRef,useState} from 'react';
import {onCacheEvent} from '../../api/client';
import {serializeDebugJSON,type DebugJSONInput,type DebugJSONSection} from '../../../shared/debug-json.mjs';

type DebugInput = Omit<DebugJSONInput,'section'>;
interface Props {
  section: DebugJSONSection;
  getSnapshot: () => DebugInput;
  isAuthorized: () => boolean;
  disabled?: boolean;
  className?: string;
}

/** Общий контрол: только копирование заранее разрешённых полей, без HTTP-запросов. */
export default function CopyDebugJSON({section,getSnapshot,isAuthorized,disabled=false,className='settings-button secondary'}:Props) {
  const [status,setStatus] = useState('');
  const [busy,setBusy] = useState(false);
  const inFlight = useRef(false),sessionEpoch = useRef(0);
  useEffect(() => onCacheEvent(event => {
    if (event.type === 'reset') {
      sessionEpoch.current++;
      setStatus('');
    }
  }),[]);
  const copy = async () => {
    if (inFlight.current || disabled) return;
    inFlight.current = true;
    setBusy(true);
    setStatus('');
    const epoch = sessionEpoch.current;
    try {
      if (!isAuthorized()) throw new Error('not_authorized');
      // Сбор данных синхронный: компонент не обращается к API или провайдерам.
      const snapshot = getSnapshot();
      if (epoch !== sessionEpoch.current || !isAuthorized()) throw new Error('session_changed');
      const json = serializeDebugJSON({...snapshot,section});
      if (epoch !== sessionEpoch.current || !isAuthorized()) throw new Error('session_changed');
      const write = navigator.clipboard?.writeText;
      if (typeof write !== 'function') throw new Error('clipboard_unavailable');
      await navigator.clipboard.writeText(json);
      if (epoch !== sessionEpoch.current || !isAuthorized()) throw new Error('session_changed');
      setStatus('JSON скопирован');
    } catch {
      if (epoch === sessionEpoch.current) setStatus('Не удалось скопировать JSON');
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  };
  return <span className="parser-debug-copy">
    <button type="button" className={className} disabled={disabled||busy}
      onClick={copy} aria-label="Копировать JSON" aria-busy={busy}>Копировать JSON</button>
    <span role="status" aria-live="polite" className="resume-hint">{status}</span>
  </span>;
}

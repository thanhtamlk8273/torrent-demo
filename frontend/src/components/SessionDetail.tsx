import {useEffect, useState} from 'react';
import {GetPeers} from '../../wailsjs/go/main/App';
import {peermgr, session} from '../../wailsjs/go/models';
import GeneralTab from './GeneralTab';
import PeerTable from './PeerTable';

interface Props {
    summary: session.SessionSummary;
}

// Single detail view for at most one session at a time, so a per-session push
// event channel is more machinery than polling warrants. The chosen tab is not
// reset when switching sessions — keeping it across selections is free.
export default function SessionDetail({summary: s}: Props) {
    const [tab, setTab] = useState<'general' | 'peers'>('general');
    const [peers, setPeers] = useState<peermgr.PeerSnapshot[]>([]);
    const [error, setError] = useState<string | null>(null);

    // Kept unconditional (not gated on tab === 'peers'): it interacts subtly
    // with the cancelled flag and the [s.ID] dependency. Possible follow-up.
    useEffect(() => {
        let cancelled = false;
        const tick = async () => {
            try {
                const list = await GetPeers(s.ID);
                if (!cancelled) setPeers(list ?? []);
            } catch (e) {
                if (!cancelled) setError(String(e));
            }
        };
        tick();
        const interval = setInterval(tick, 1500);
        return () => {
            cancelled = true;
            clearInterval(interval);
        };
    }, [s.ID]);

    return (
        <div className="session-detail">
            <div className="detail-head">
                <h2>{s.Name}</h2>
                <div className="tab-bar">
                    <button className={`tab ${tab === 'general' ? 'active' : ''}`} onClick={() => setTab('general')}>
                        <span className="tab-glyph">ℹ</span><span>General</span>
                    </button>
                    <button className={`tab ${tab === 'peers' ? 'active' : ''}`} onClick={() => setTab('peers')}>
                        <span className="tab-glyph">⇅</span><span>Peers</span>
                    </button>
                </div>
            </div>
            {error && <div className="error-banner">{error}</div>}
            {tab === 'general' ? (
                <GeneralTab summary={s}/>
            ) : (
                <PeerTable peers={peers}/>
            )}
        </div>
    );
}

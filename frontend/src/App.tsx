import {useEffect, useState} from 'react';
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime';
import {
    AddTorrent,
    ListSessions,
    PickTorrentFile,
    RemoveSession,
    StartSession,
    StopSession,
} from '../wailsjs/go/main/App';
import {session} from '../wailsjs/go/models';
import SessionTable from './components/SessionTable';
import SessionDetail from './components/SessionDetail';
import {formatRate} from './format';

function App() {
    const [sessions, setSessions] = useState<session.SessionSummary[]>([]);
    const [selectedId, setSelectedId] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    // One ListSessions() on mount so the first paint isn't empty before the
    // first sessions:update tick; afterwards the push event is the only
    // source of live session state.
    useEffect(() => {
        ListSessions().then(setSessions).catch((e) => setError(String(e)));
        EventsOn("sessions:update", (list: session.SessionSummary[]) => setSessions(list));
        return () => EventsOff("sessions:update");
    }, []);

    // If the selected session was removed, drop the stale selection.
    useEffect(() => {
        if (selectedId && !sessions.some((s) => s.ID === selectedId)) {
            setSelectedId(null);
        }
    }, [sessions, selectedId]);

    const selected = sessions.find((s) => s.ID === selectedId) ?? null;

    // Header aggregate stats, computed client-side from the sessions array.
    const totals = sessions.reduce(
        (acc, s) => ({
            running: acc.running + (s.Status === 'Running' ? 1 : 0),
            down: acc.down + s.DownloadRate,
            up: acc.up + s.UploadRate,
        }),
        {running: 0, down: 0, up: 0},
    );

    const handleAdd = async () => {
        try {
            const path = await PickTorrentFile();
            if (!path) return; // dialog cancelled
            await AddTorrent(path);
        } catch (e) {
            setError(String(e));
        }
    };

    const act = (fn: (id: string) => Promise<void>, label: string) => async () => {
        if (!selectedId) return;
        try {
            await fn(selectedId);
        } catch (e) {
            setError(`${label}: ${e}`);
        }
    };

    return (
        <div id="App">
            <div className="toolbar">
                <button className="toolbar-button" onClick={handleAdd}>＋ Add</button>
                <button
                    className="toolbar-button"
                    disabled={!selected || selected.Status !== 'Pending'}
                    onClick={act((id) => StartSession(id), 'start')}
                >▶ Start</button>
                <button
                    className="toolbar-button"
                    disabled={!selected || selected.Status !== 'Running'}
                    onClick={act((id) => StopSession(id), 'stop')}
                >■ Stop</button>
                <button
                    className="toolbar-button"
                    disabled={!selected || selected.Status === 'Running'}
                    onClick={act((id) => RemoveSession(id), 'remove')}
                >✕ Remove</button>
                <span className="header-stats">
                    {totals.running} running / {sessions.length} total · ↓ {formatRate(totals.down)} · ↑ {formatRate(totals.up)}
                </span>
            </div>

            {error && (
                <div className="error-banner">
                    <span>{error}</span>
                    <button onClick={() => setError(null)} aria-label="dismiss">×</button>
                </div>
            )}

            <div className="main-layout">
                <div className="table-panel">
                    <SessionTable sessions={sessions} selectedId={selectedId} onSelect={setSelectedId}/>
                </div>
                <div className="detail-panel">
                    {selected ? (
                        <SessionDetail summary={selected}/>
                    ) : (
                        <div className="empty-hint">Select a torrent to see its details.</div>
                    )}
                </div>
            </div>
        </div>
    );
}

export default App;

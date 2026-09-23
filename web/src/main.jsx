import React, { useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { Excalidraw, restore, serializeAsJSON } from "@excalidraw/excalidraw";
import "@excalidraw/excalidraw/index.css";
import "./style.css";

const clientId = `browser-${crypto.randomUUID()}`;

function restoreForEditor(scene, api) {
  const sanitized = JSON.parse(serializeAsJSON(
    scene.elements || [],
    scene.appState || {},
    scene.files || {},
    "local",
  ));
  return restore(
    sanitized,
    api ? api.getAppState() : null,
    api ? api.getSceneElements() : null,
  );
}

function App() {
  const [initialScene, setInitialScene] = useState(null);
  const [status, setStatus] = useState("Connecting");
  const apiRef = useRef(null);
  const revisionRef = useRef(0);
  const saveTimerRef = useRef(null);
  const pendingSceneRef = useRef(null);
  const suppressUntilRef = useRef(0);
  const lastSceneJSONRef = useRef("");

  const applySnapshot = useCallback((snapshot, source = "server") => {
    revisionRef.current = snapshot.revision;
    const api = apiRef.current;
    if (source === clientId && api) {
      setStatus(`Saved · revision ${snapshot.revision}`);
      return;
    }
    const restored = restoreForEditor(snapshot.scene, api);
    if (!api) {
      setInitialScene(restored);
      setStatus(`Connected · revision ${snapshot.revision}`);
      return;
    }
    if (saveTimerRef.current) {
      clearTimeout(saveTimerRef.current);
      saveTimerRef.current = null;
    }
    suppressUntilRef.current = Date.now() + 250;
    api.updateScene({
      elements: restored.elements,
      appState: restored.appState,
      commitToHistory: true,
    });
    const files = Object.values(restored.files || {});
    if (files.length > 0) api.addFiles(files);
    setStatus(`Synced · revision ${snapshot.revision}`);
  }, []);

  const reload = useCallback(async () => {
    const response = await fetch("/api/scene", { cache: "no-store" });
    if (!response.ok) throw new Error(`load failed: HTTP ${response.status}`);
    applySnapshot(await response.json());
  }, [applySnapshot]);

  useEffect(() => {
    reload().catch((error) => setStatus(error.message));
    const events = new EventSource("/api/events");
    events.addEventListener("scene", (message) => {
      try {
        const event = JSON.parse(message.data);
        applySnapshot({ revision: event.revision, scene: event.scene }, event.source);
      } catch (error) {
        setStatus(`Sync error · ${error.message}`);
      }
    });
    events.onerror = () => setStatus("Reconnecting");
    return () => {
      events.close();
      clearTimeout(saveTimerRef.current);
    };
  }, [applySnapshot, reload]);

  const save = useCallback(async () => {
    const scene = pendingSceneRef.current;
    if (!scene) return;
    const expectedRevision = revisionRef.current;
    setStatus("Saving");
    try {
      const response = await fetch("/api/scene", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ scene, expectedRevision, clientId }),
      });
      const snapshot = await response.json();
      if (response.status === 409) {
        applySnapshot(snapshot, "conflict");
        setStatus(`Conflict resolved · revision ${snapshot.revision}`);
        return;
      }
      if (!response.ok) throw new Error(snapshot.error || `HTTP ${response.status}`);
      revisionRef.current = snapshot.revision;
      setStatus(`Saved · revision ${snapshot.revision}`);
    } catch (error) {
      setStatus(`Save failed · ${error.message}`);
    }
  }, [applySnapshot]);

  const onChange = useCallback((elements, appState, files) => {
    if (Date.now() < suppressUntilRef.current) return;
    const serialized = serializeAsJSON(elements, appState, files, "local");
    if (serialized === lastSceneJSONRef.current) return;
    lastSceneJSONRef.current = serialized;
    pendingSceneRef.current = JSON.parse(serialized);
    clearTimeout(saveTimerRef.current);
    saveTimerRef.current = setTimeout(save, 300);
  }, [save]);

  if (!initialScene) {
    return <main className="loading"><strong>Excalidraw MCP</strong><span>{status}</span></main>;
  }

  return (
    <main className="editor">
      <div className="status" role="status">{status}</div>
      <Excalidraw
        initialData={initialScene}
        excalidrawAPI={(api) => { apiRef.current = api; }}
        onChange={onChange}
        UIOptions={{ canvasActions: { loadScene: false } }}
      />
    </main>
  );
}

createRoot(document.getElementById("root")).render(
  <React.StrictMode><App /></React.StrictMode>,
);

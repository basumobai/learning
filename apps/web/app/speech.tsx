"use client";
import { useEffect, useRef, useState } from "react";
import { api, speechBlob, type Model, type Preferences } from "../lib/api";
import { useAuth } from "./providers";

export function useModels() {
  const { user } = useAuth();
  const [models, setModels] = useState<Model[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!user || user.must_change_password) return;
    let live = true;
    api<{ models: Model[] }>("/ai/models")
      .then((d) => {
        if (live) setModels(d.models);
      })
      .catch((e) => {
        if (live) setError(e.message);
      });
    return () => {
      live = false;
    };
  }, [user]);
  return { models, error };
}
export function useVoice() {
  const { models, error } = useModels();
  const { user } = useAuth();
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [voiceError, setError] = useState("");
  useEffect(() => {
    if (user && !user.must_change_password)
      api<Preferences>("/preferences")
        .then(setPrefs)
        .catch((e) => setError(e.message));
  }, [user]);
  const speechModels = models.filter((m) => m.kind === "tts");
  const model =
    speechModels.find((m) => m.id === prefs?.tts_model_id) || speechModels[0];
  const voice = model?.voices.includes(prefs?.voice || "")
    ? prefs!.voice
    : model?.voices[0] || "";
  async function change(modelID: number, nextVoice: string) {
    if (!prefs) return;
    const next = { ...prefs, tts_model_id: modelID, voice: nextVoice };
    setPrefs(next);
    try {
      setPrefs(
        await api<Preferences>("/preferences/voice", {
          model_id: modelID,
          voice: nextVoice,
        }),
      );
      setError("");
    } catch (e) {
      setError((e as Error).message);
    }
  }
  return {
    models,
    speechModels,
    model,
    voice,
    error: error || voiceError,
    change,
    body: { model_id: model?.id || 0, voice },
  };
}
export function VoicePicker({
  config,
}: {
  config: ReturnType<typeof useVoice>;
}) {
  return (
    <div className="voice-picker">
      {config.speechModels.length ? (
        <>
          <label>
            语音模型
            <select
              value={config.model?.id || ""}
              onChange={(e) => {
                const m = config.speechModels.find(
                  (m) => m.id === Number(e.target.value),
                );
                if (m) config.change(m.id, m.voices[0]);
              }}
            >
              {config.speechModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            音色
            <select
              value={config.voice}
              onChange={(e) => config.change(config.model!.id, e.target.value)}
            >
              {config.model?.voices.map((v) => (
                <option key={v}>{v}</option>
              ))}
            </select>
          </label>
        </>
      ) : (
        <p className="setting-note">尚未授权语音模型，请联系管理员配置。</p>
      )}
      {config.error && <p role="alert">{config.error}</p>}
    </div>
  );
}
export function useSpeech() {
  const [url, setURL] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [speed, setSpeed] = useState(1);
  const audio = useRef<HTMLAudioElement>(null);
  const cache = useRef(new Map<string, string>());
  const generation = useRef(0);
  useEffect(
    () => () => {
      generation.current++;
      cache.current.forEach((u) => URL.revokeObjectURL(u));
      cache.current.clear();
    },
    [],
  );
  useEffect(() => {
    if (audio.current) audio.current.playbackRate = speed;
  }, [speed, url]);
  useEffect(() => {
    if (url) audio.current?.play().catch(() => {});
  }, [url]);
  async function play(path: string, body: object) {
    if (busy) return;
    const mine = ++generation.current;
    setBusy(true);
    setError("");
    audio.current?.pause();
    try {
      const key = path + JSON.stringify(body);
      let next = cache.current.get(key);
      if (!next) {
        const blob = await speechBlob(path, body);
        if (mine !== generation.current) return;
        next = URL.createObjectURL(blob);
        cache.current.set(key, next);
      }
      if (mine !== generation.current) return;
      if (url === next) {
        if (audio.current) audio.current.currentTime = 0;
        await audio.current?.play();
      } else setURL(next);
    } catch (e) {
      if (mine === generation.current) setError((e as Error).message);
    } finally {
      if (mine === generation.current) setBusy(false);
    }
  }
  function stop() {
    generation.current++;
    setBusy(false);
    audio.current?.pause();
    setURL("");
  }
  return { url, busy, error, audio, speed, setSpeed, play, stop };
}
export function AudioOutput({
  player,
}: {
  player: ReturnType<typeof useSpeech>;
}) {
  return (
    <div className="audio-output">
      {player.busy && <p role="status">正在准备自然语音…</p>}
      {player.url && (
        <>
          <audio
            ref={player.audio}
            src={player.url}
            controls
            aria-label="AI 生成的学习语音"
          />
          <label>
            播放速度
            <select
              value={player.speed}
              onChange={(e) => player.setSpeed(Number(e.target.value))}
            >
              {[0.75, 0.9, 1, 1.1, 1.25].map((n) => (
                <option key={n} value={n}>
                  {n} 倍
                </option>
              ))}
            </select>
          </label>
          <small>AI 生成语音</small>
        </>
      )}
      {player.error && (
        <p className="error" role="alert">
          {player.error}
        </p>
      )}
    </div>
  );
}

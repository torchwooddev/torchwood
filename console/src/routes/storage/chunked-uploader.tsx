import { useCallback, useEffect, useRef, useState } from "react";
import {
  createUploadSession,
  getUploadSession,
  uploadChunk,
  completeUpload,
  CHUNK_SIZE,
  type FileItem,
} from "@/api/storage";
import { Progress } from "@/components/ui/progress";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const k = 1024;
  const sizes = ["B", "KB", "MB", "GB"];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
}

interface ChunkedUploaderProps {
  bucketId: string;
  file: File;
  onSuccess: (file: FileItem) => void;
  onError: (message: string) => void;
}

function uploadKey(bucketId: string, file: File): string {
  return `torchwood:upload:${bucketId}:${file.name}:${file.size}`;
}

/**
 * ChunkedUploader 大文件（> 16MiB）分片上传：
 * uploadId 存 localStorage（键含 bucketId+fileName+size），页面刷新/失败后可续传；
 * 续传时 getUploadSession 跳过已收分片；全部完成 → completeUpload → 清除 localStorage。
 */
export function ChunkedUploader({ bucketId, file, onSuccess, onError }: ChunkedUploaderProps) {
  const [uploaded, setUploaded] = useState(0);
  const [total, setTotal] = useState(0);
  const [failed, setFailed] = useState(false);
  const startedRef = useRef(false);

  const run = useCallback(async () => {
    const key = uploadKey(bucketId, file);
    const size = file.size;
    let uploadId = localStorage.getItem(key);

    const start = async (): Promise<{ upload_id: string; part_count: number }> => {
      if (uploadId) {
        try {
          const progress = await getUploadSession(bucketId, uploadId);
          setTotal(progress.part_count);
          return { upload_id: uploadId, part_count: progress.part_count };
        } catch {
          // 会话过期/不存在：重建。
          localStorage.removeItem(key);
          uploadId = null;
        }
      }
      const session = await createUploadSession(bucketId, {
        name: file.name,
        mime_type: file.type || "",
        size,
      });
      localStorage.setItem(key, session.upload_id);
      setTotal(session.part_count);
      return { upload_id: session.upload_id, part_count: session.part_count };
    };

    try {
      const { upload_id: uid, part_count } = await start();
      const progress = await getUploadSession(bucketId, uid);
      const received = new Set(progress.received);

      let done = 0;
      for (let part = 1; part <= part_count; part++) {
        if (received.has(part)) {
          done++;
          setUploaded(done);
          continue;
        }
        const blob = file.slice((part - 1) * CHUNK_SIZE, Math.min(part * CHUNK_SIZE, size));
        await uploadChunk(bucketId, uid, part, blob);
        done++;
        setUploaded(done);
      }

      const completed = await completeUpload(bucketId, uid);
      localStorage.removeItem(key);
      onSuccess(completed);
    } catch (err) {
      setFailed(true);
      onError(
        err instanceof Error && err.message
          ? err.message
          : `上传中断：已上传 ${uploaded}/${total || "?"} 片，可重新选择文件续传`
      );
    }
  }, [bucketId, file, onSuccess, onError, uploaded, total]);

  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;
    void run();
  }, [run]);

  if (failed) {
    return null;
  }

  const percent = total > 0 ? Math.round((uploaded / total) * 100) : 0;
  return (
    <div className="w-full max-w-xs space-y-1.5">
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span className="truncate pr-2">{file.name}</span>
        <span>
          {formatBytes(file.size)} · {percent}%
        </span>
      </div>
      <Progress value={percent} />
      <p className="text-xs text-muted-foreground">
        分片上传中（{uploaded}/{total}）…
      </p>
    </div>
  );
}

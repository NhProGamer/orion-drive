/** The upload-session fields the chunk driver needs (a subset of UploadInit,
 *  so both the drive and share init responses satisfy it). */
export interface ChunkInit {
  session_id: string
  chunk_size: number
  num_chunks: number
}

/** The three calls a chunked upload needs, so the same driver works for the
 *  authenticated drive and for public share uploads. */
export interface ChunkUploader {
  init(): Promise<ChunkInit>
  putChunk(sessionId: string, index: number, blob: Blob, onProgress?: (sent: number) => void): Promise<unknown>
  complete(sessionId: string): Promise<unknown>
}

/**
 * uploadInChunks streams a file through a resumable chunked upload with bounded
 * concurrency: several chunks are in flight at once so throughput is not capped
 * by the round-trip latency of one chunk at a time (a big win on WAN links).
 * The server writes each chunk at its offset, so out-of-order completion is
 * safe. `onLoaded` receives the cumulative bytes sent across all chunks.
 */
export async function uploadInChunks(
  file: File,
  uploader: ChunkUploader,
  onLoaded: (loaded: number) => void,
  concurrency = 4,
): Promise<void> {
  const { session_id, chunk_size, num_chunks } = await uploader.init()
  const sentPer = new Array<number>(num_chunks).fill(0)
  const report = () => onLoaded(sentPer.reduce((a, b) => a + b, 0))
  let next = 0
  const worker = async () => {
    for (let i = next++; i < num_chunks; i = next++) {
      const start = i * chunk_size
      const end = Math.min(file.size, start + chunk_size)
      await uploader.putChunk(session_id, i, file.slice(start, end), (sent) => {
        sentPer[i] = sent
        report()
      })
      sentPer[i] = end - start
      report()
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, num_chunks) }, () => worker()))
  await uploader.complete(session_id)
}

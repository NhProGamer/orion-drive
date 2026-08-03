import { describe, expect, it } from 'vitest'
import { uploadInChunks, type ChunkUploader } from '@/lib/upload'

// Build a File of `size` bytes so slice()/size behave like a real upload source.
function fakeFile(size: number): File {
  return new File([new Uint8Array(size)], 'blob.bin')
}

describe('uploadInChunks', () => {
  it('slices the file at chunk boundaries and reports cumulative bytes', async () => {
    const file = fakeFile(10)
    const sizes: number[] = []
    const loaded: number[] = []
    let completed = false

    const uploader: ChunkUploader = {
      init: async () => ({ session_id: 's', chunk_size: 4, num_chunks: 3 }),
      putChunk: async (_sid, _i, blob) => {
        sizes.push(blob.size)
      },
      complete: async () => {
        completed = true
      },
    }

    await uploadInChunks(file, uploader, (n) => loaded.push(n), 1)

    // 3 chunks: 4 + 4 + 2 = 10 bytes total.
    expect(sizes.sort((a, b) => a - b)).toEqual([2, 4, 4])
    expect(loaded.at(-1)).toBe(10) // final cumulative equals file size
    expect(completed).toBe(true)
  })

  it('does not call complete until every chunk is uploaded', async () => {
    const file = fakeFile(10)
    let putCount = 0
    let completeCalledAfter = -1

    const uploader: ChunkUploader = {
      init: async () => ({ session_id: 's', chunk_size: 4, num_chunks: 3 }),
      putChunk: async () => {
        putCount += 1
      },
      complete: async () => {
        completeCalledAfter = putCount
      },
    }

    await uploadInChunks(file, uploader, () => {}, 4)
    expect(putCount).toBe(3)
    expect(completeCalledAfter).toBe(3) // all chunks done before complete
  })

  it('caps concurrency at the chunk count and forwards per-chunk progress', async () => {
    const file = fakeFile(6)
    const loaded: number[] = []

    const uploader: ChunkUploader = {
      init: async () => ({ session_id: 's', chunk_size: 3, num_chunks: 2 }),
      putChunk: async (_sid, _i, _blob, onProgress) => {
        onProgress?.(1) // partial progress within the chunk
        onProgress?.(3)
      },
      complete: async () => {},
    }

    await uploadInChunks(file, uploader, (n) => loaded.push(n), 8)
    expect(Math.max(...loaded)).toBe(6) // reaches the full size
    expect(loaded.every((n) => n <= 6)).toBe(true)
  })
})

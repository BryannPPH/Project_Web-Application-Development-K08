import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { MigrationEditor } from '@/modules/migrations/MigrationEditor'

afterEach(cleanup)

describe('MigrationEditor', () => {
  it('menolak nama dan SQL kosong', () => {
    const submit = vi.fn()
    render(<MigrationEditor onSubmit={submit} submitLabel="Simpan" />)

    fireEvent.click(screen.getByRole('button', { name: 'Simpan' }))

    expect(screen.getByRole('alert')).toHaveTextContent('wajib diisi')
    expect(submit).not.toHaveBeenCalled()
  })

  it('mengirim draft yang valid', () => {
    const submit = vi.fn()
    render(<MigrationEditor onSubmit={submit} submitLabel="Simpan" />)

    fireEvent.change(screen.getByLabelText('Nama migration'), { target: { value: 'create widgets' } })
    fireEvent.change(screen.getByLabelText('Up SQL'), { target: { value: 'CREATE TABLE widgets(id bigint)' } })
    fireEvent.change(screen.getByLabelText('Down SQL'), { target: { value: 'DROP TABLE widgets' } })
    fireEvent.click(screen.getByRole('button', { name: 'Simpan' }))

    expect(submit).toHaveBeenCalledWith({
      name: 'create widgets', sql: 'CREATE TABLE widgets(id bigint)', downSql: 'DROP TABLE widgets', executionMode: 'single',
    })
  })

  it('mempertahankan definisi batch ketika nama draft diedit', () => {
    const submit = vi.fn()
    const definition = {
      name: 'Batch',
      sql: 'UPDATE widgets SET value=1 WHERE id>$1 AND id<=$2',
      downSql: 'UPDATE widgets SET value=0 WHERE id>$1 AND id<=$2',
      executionMode: 'batch' as const,
      batchSelectSql: 'SELECT id FROM widgets WHERE id>$1 ORDER BY id LIMIT $2',
      batchSize: 25,
      batchCursorStart: -1,
    }
    render(<MigrationEditor initialValue={definition} onSubmit={submit} submitLabel="Simpan" />)

    fireEvent.change(screen.getByLabelText('Nama migration'), { target: { value: 'Nama baru' } })
    fireEvent.click(screen.getByRole('button', { name: 'Simpan' }))

    expect(submit).toHaveBeenCalledWith({ ...definition, name: 'Nama baru' })
  })

  it('meminta alasan bila migration irreversible', () => {
    const submit = vi.fn()
    render(<MigrationEditor initialValue={{ name: 'Hapus tabel', sql: 'DROP TABLE widgets' }} onSubmit={submit} submitLabel="Simpan" />)

    fireEvent.change(screen.getByLabelText('Strategi rollback'), { target: { value: 'irreversible' } })
    fireEvent.click(screen.getByRole('button', { name: 'Simpan' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Alasan irreversible wajib diisi')
    expect(submit).not.toHaveBeenCalled()

    fireEvent.change(screen.getByLabelText('Alasan irreversible'), { target: { value: 'Data sementara' } })
    fireEvent.click(screen.getByRole('button', { name: 'Simpan' }))
    expect(submit).toHaveBeenCalledWith({
      name: 'Hapus tabel', sql: 'DROP TABLE widgets', executionMode: 'single', irreversibleReason: 'Data sementara',
    })
  })

  it('readonly untuk history terminal', () => {
    render(
      <MigrationEditor
        disabled
        initialValue={{ name: 'done', sql: 'SELECT 1' }}
        onSubmit={vi.fn()}
        submitLabel="Simpan"
      />,
    )

    expect(screen.getByLabelText('Nama migration')).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Simpan' })).not.toBeInTheDocument()
  })

  it('menampilkan aturan DDL non-transaksional berdasarkan capability target', () => {
    render(<MigrationEditor onSubmit={vi.fn()} submitLabel="Simpan" transactionalDdl={false} />)

    expect(screen.getByText(/DML \(SELECT, INSERT, UPDATE, DELETE\) dijalankan dalam satu transaksi/)).toBeInTheDocument()
    expect(screen.getByText(/DDL didukung terpisah, tetapi melakukan implicit commit/)).toBeInTheDocument()
  })
})

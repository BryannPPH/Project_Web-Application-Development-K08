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
    fireEvent.change(screen.getByLabelText('SQL'), { target: { value: 'CREATE TABLE widgets(id bigint)' } })
    fireEvent.click(screen.getByRole('button', { name: 'Simpan' }))

    expect(submit).toHaveBeenCalledWith({ name: 'create widgets', sql: 'CREATE TABLE widgets(id bigint)' })
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

  it('menampilkan batas DDL berdasarkan capability target', () => {
    render(<MigrationEditor onSubmit={vi.fn()} submitLabel="Simpan" transactionalDdl={false} />)

    expect(screen.getByText(/SELECT, INSERT, UPDATE, dan DELETE dapat dijalankan/)).toBeInTheDocument()
    expect(screen.getByText(/DDL \(CREATE, ALTER, DROP\) ditolak/)).toBeInTheDocument()
  })
})

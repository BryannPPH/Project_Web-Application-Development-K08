import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { apiRequest } from '@/lib/api-client'
import type { PlatformRole } from '@/modules/auth/types'
import type { AccessLevel } from '@/modules/databases/types'
import type {
  AdminGroup,
  AdminUser,
  ConnectionTestResult,
  GroupGrant,
  GroupMember,
  ManagedDatabase,
  ManagedDatabaseInput,
} from '@/modules/admin/types'

export const adminQueryKeys = {
  all: ['admin'] as const,
  users: ['admin', 'users'] as const,
  groups: ['admin', 'groups'] as const,
  members: (groupId: string) => ['admin', 'groups', groupId, 'members'] as const,
  groupGrants: (groupId: string) => ['admin', 'groups', groupId, 'grants'] as const,
  databases: ['admin', 'databases'] as const,
}

const id = encodeURIComponent

// ---- Users ----

export function useAdminUsers() {
  return useQuery({
    queryKey: adminQueryKeys.users,
    queryFn: async ({ signal }) =>
      (await apiRequest<{ users: AdminUser[] }>('/admin/users', { signal })).users,
  })
}

export interface CreateUserInput {
  username: string
  password: string
  platformRole: PlatformRole
}

export interface UpdateUserInput {
  username?: string
  platformRole?: PlatformRole
  isActive?: boolean
}

export function useUserMutations() {
  const queryClient = useQueryClient()
  // Membership menampilkan status user, jadi invalidasi seluruh cache admin.
  const invalidate = () => queryClient.invalidateQueries({ queryKey: adminQueryKeys.all })

  return {
    create: useMutation({
      mutationFn: (input: CreateUserInput) =>
        apiRequest<{ user: AdminUser }>('/admin/users', { method: 'POST', json: input }),
      onSuccess: invalidate,
    }),
    update: useMutation({
      mutationFn: ({ userId, input }: { userId: string; input: UpdateUserInput }) =>
        apiRequest<{ user: AdminUser }>(`/admin/users/${id(userId)}`, { method: 'PATCH', json: input }),
      onSuccess: invalidate,
    }),
    resetPassword: useMutation({
      mutationFn: ({ userId, password }: { userId: string; password: string }) =>
        apiRequest<{ user: AdminUser }>(`/admin/users/${id(userId)}/reset-password`, {
          method: 'POST',
          json: { password },
        }),
      onSuccess: invalidate,
    }),
  }
}

// ---- Groups, members, grants ----

export function useAdminGroups() {
  return useQuery({
    queryKey: adminQueryKeys.groups,
    queryFn: async ({ signal }) =>
      (await apiRequest<{ groups: AdminGroup[] }>('/admin/groups', { signal })).groups,
  })
}

export function useGroupMembers(groupId: string) {
  return useQuery({
    queryKey: adminQueryKeys.members(groupId),
    queryFn: async ({ signal }) =>
      (await apiRequest<{ members: GroupMember[] }>(`/admin/groups/${id(groupId)}/members`, { signal }))
        .members,
  })
}

export function useGroupGrants(groupId: string) {
  return useQuery({
    queryKey: adminQueryKeys.groupGrants(groupId),
    queryFn: async ({ signal }) =>
      (await apiRequest<{ grants: GroupGrant[] }>(`/admin/groups/${id(groupId)}/databases`, { signal }))
        .grants,
  })
}

export function useGroupMutations() {
  const queryClient = useQueryClient()
  const invalidate = () => queryClient.invalidateQueries({ queryKey: adminQueryKeys.all })

  return {
    create: useMutation({
      mutationFn: (input: { name: string; description?: string }) =>
        apiRequest<{ group: AdminGroup }>('/admin/groups', { method: 'POST', json: input }),
      onSuccess: invalidate,
    }),
    update: useMutation({
      mutationFn: ({ groupId, input }: { groupId: string; input: { name?: string; description?: string } }) =>
        apiRequest<{ group: AdminGroup }>(`/admin/groups/${id(groupId)}`, { method: 'PATCH', json: input }),
      onSuccess: invalidate,
    }),
    remove: useMutation({
      mutationFn: (groupId: string) =>
        apiRequest<void>(`/admin/groups/${id(groupId)}`, { method: 'DELETE' }),
      onSuccess: invalidate,
    }),
    addMember: useMutation({
      mutationFn: ({ groupId, userId }: { groupId: string; userId: string }) =>
        apiRequest<{ added: boolean }>(`/admin/groups/${id(groupId)}/members/${id(userId)}`, {
          method: 'POST',
        }),
      onSuccess: invalidate,
    }),
    removeMember: useMutation({
      mutationFn: ({ groupId, userId }: { groupId: string; userId: string }) =>
        apiRequest<{ removed: boolean }>(`/admin/groups/${id(groupId)}/members/${id(userId)}`, {
          method: 'DELETE',
        }),
      onSuccess: invalidate,
    }),
    upsertGrant: useMutation({
      mutationFn: ({
        groupId,
        databaseId,
        accessLevel,
      }: {
        groupId: string
        databaseId: string
        accessLevel: AccessLevel
      }) =>
        apiRequest<{ grant: GroupGrant }>(`/admin/groups/${id(groupId)}/databases/${id(databaseId)}`, {
          method: 'PUT',
          json: { accessLevel },
        }),
      onSuccess: invalidate,
    }),
    revokeGrant: useMutation({
      mutationFn: ({ groupId, databaseId }: { groupId: string; databaseId: string }) =>
        apiRequest<void>(`/admin/groups/${id(groupId)}/databases/${id(databaseId)}`, {
          method: 'DELETE',
        }),
      onSuccess: invalidate,
    }),
  }
}

// ---- Managed databases ----

export function useManagedDatabases() {
  return useQuery({
    queryKey: adminQueryKeys.databases,
    queryFn: async ({ signal }) =>
      (await apiRequest<{ databases: ManagedDatabase[] }>('/admin/databases', { signal })).databases,
  })
}

export function useManagedDatabaseMutations() {
  const queryClient = useQueryClient()
  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: adminQueryKeys.all })
    void queryClient.invalidateQueries({ queryKey: ['databases'] })
  }

  return {
    create: useMutation({
      mutationFn: (input: ManagedDatabaseInput) =>
        apiRequest<{ database: ManagedDatabase }>('/admin/databases', { method: 'POST', json: input }),
      onSuccess: invalidate,
    }),
    update: useMutation({
      mutationFn: ({ databaseId, input }: { databaseId: string; input: Partial<ManagedDatabaseInput> }) =>
        apiRequest<{ database: ManagedDatabase }>(`/admin/databases/${id(databaseId)}`, {
          method: 'PATCH',
          json: input,
        }),
      onSuccess: invalidate,
    }),
    remove: useMutation({
      mutationFn: (databaseId: string) =>
        apiRequest<void>(`/admin/databases/${id(databaseId)}`, { method: 'DELETE' }),
      onSuccess: invalidate,
    }),
    testStored: useMutation({
      mutationFn: (databaseId: string) =>
        apiRequest<ConnectionTestResult>(`/admin/databases/${id(databaseId)}/test`, { method: 'POST' }),
    }),
    testDraft: useMutation({
      mutationFn: (input: ManagedDatabaseInput) =>
        apiRequest<ConnectionTestResult>('/admin/databases/test-connection', { method: 'POST', json: input }),
    }),
  }
}

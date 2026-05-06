import axios from 'axios';
import { User } from '../types/user';

const API_BASE = '/api/v1';

const client = axios.create({
  baseURL: API_BASE,
  timeout: 10000,
});

export async function fetchUsers(): Promise<User[]> {
  const response = await client.get<User[]>('/users');
  return response.data;
}

export async function createUser(user: Omit<User, 'id'>): Promise<User> {
  const response = await client.post<User>('/users', user);
  return response.data;
}

export async function deleteUser(userId: number): Promise<void> {
  await client.delete(`/users/${userId}`);
}

export interface User {
  id: number;
  name: string;
  email: string;
  createdAt: Date;
}

export interface UserCreateRequest {
  name: string;
  email: string;
}

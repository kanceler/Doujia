import { Controller, Get, Post, Query, Param, Body, NotFoundException } from '@nestjs/common';
import { NeedLogin } from '@lark-apaas/fullstack-nestjs-core';
import { TemplateRegistrationService } from './template-registration.service';
import type {
  RegistrationStateItem,
  PipelineTemplateItem,
  PipelineTemplateDsl,
  SaveConfigRequest,
} from '@shared/api.interface';

@Controller('api/registration-state')
export class RegistrationStateController {
  constructor(private readonly service: TemplateRegistrationService) {}

  @Get()
  async getRegistrationStates(
    @Query('type') type?: string,
  ): Promise<{ items: RegistrationStateItem[] }> {
    return this.service.getRegistrationStates(type);
  }
}

@Controller('api/pipeline-templates')
export class PipelineTemplateController {
  constructor(private readonly service: TemplateRegistrationService) {}

  @Get()
  async getTemplates(): Promise<{ items: PipelineTemplateItem[] }> {
    return this.service.getTemplates();
  }

  @Get('/:templateId/dsl')
  async getTemplateDsl(
    @Param('templateId') templateId: string,
  ): Promise<PipelineTemplateDsl> {
    const result: PipelineTemplateDsl | null = await this.service.getTemplateDsl(templateId);
    if (!result) {
      throw new NotFoundException(`Pipeline template not found: ${templateId}`);
    }
    return result;
  }
}

@Controller('api/config')
export class ConfigController {
  constructor(private readonly service: TemplateRegistrationService) {}

  @Post()
  @NeedLogin()
  async saveConfig(
    @Body() body: SaveConfigRequest,
  ): Promise<{ success: boolean }> {
    return this.service.saveConfig(body);
  }
}
